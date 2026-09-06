// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"slices"
	"strings"
	"time"

	marmot "github.com/marmotdata/marmot/sdk/go"
	"github.com/marmotdata/marmot/sdk/go/auth"
)

// iamClient talks to the access-policy endpoints directly rather than through
// the generated SDK, which does not yet cover them. It reuses the SDK client's
// resolved host and credential so the provider keeps one authentication story.
type iamClient struct {
	host string
	cred auth.Credential
	http *http.Client
}

func newIAMClient(c *marmot.Client) *iamClient {
	return &iamClient{
		host: strings.TrimSuffix(c.Host(), "/"),
		cred: c.Credential(),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// iamBinding is one role and the members holding it.
type iamBinding struct {
	Role    string   `json:"role"`
	Members []string `json:"members"`
}

// iamPolicy is the complete set of bindings on a resource.
//
// Etag is what makes concurrent applies safe: the server rejects a write built
// on a stale read, so two runs touching the same resource conflict loudly
// instead of one silently discarding the other's bindings.
type iamPolicy struct {
	// omitempty so a rendered policy document carries no etag: the data source
	// builds a document that has never been read from a server, and an empty
	// etag in the output would read as a value rather than as its absence.
	Etag     string       `json:"etag,omitempty"`
	Bindings []iamBinding `json:"bindings"`
}

type setPolicyRequest struct {
	Policy iamPolicy `json:"policy"`
}

// errPolicyConflict is returned when the policy changed between read and write.
var errPolicyConflict = errors.New("iam policy changed since it was read")

// errNoPolicyAPI means the instance does not serve the access-policy endpoints.
//
// Marmot answers 501; builds from before it had that case fall through to a
// bare 404. Neither is ambiguous: the endpoints answer 400 for a resource type
// they do not know and 200 with an empty policy even when the resource has been
// deleted, so they never produce a 404 of their own.
type errNoPolicyAPI struct {
	host   string
	status int
}

func (e *errNoPolicyAPI) Error() string {
	return fmt.Sprintf("%s: no access-policy API (HTTP %d). Access grants require "+
		"Marmot Cloud or Marmot Enterprise (https://cloud.marmotdata.io). If this is not "+
		"the instance you meant to reach, check the provider's host setting; otherwise "+
		"remove the marmot_*_iam_* resources from this configuration", e.host, e.status)
}

// noPolicyAPI reports whether a status means the endpoints are absent.
func (c *iamClient) noPolicyAPI(status int) (*errNoPolicyAPI, bool) {
	if status != http.StatusNotImplemented && status != http.StatusNotFound {
		return nil, false
	}
	return &errNoPolicyAPI{host: c.host, status: status}, true
}

func (c *iamClient) policyURL(resourceType, resourceID string) string {
	if resourceID == "" {
		resourceID = "-"
	}
	return fmt.Sprintf("%s/api/v1/iam/%s/%s/policy", c.host, resourceType, resourceID)
}

func (c *iamClient) do(ctx context.Context, method, url string, body any) ([]byte, int, error) {
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("encoding request: %w", err)
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	switch c.cred.Scheme() {
	case auth.SchemeBearer:
		req.Header.Set("Authorization", "Bearer "+c.cred.Token())
	default:
		req.Header.Set("X-API-Key", c.cred.Token())
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return payload, resp.StatusCode, nil
}

func (c *iamClient) GetPolicy(ctx context.Context, resourceType, resourceID string) (*iamPolicy, error) {
	payload, status, err := c.do(ctx, http.MethodGet, c.policyURL(resourceType, resourceID), nil)
	if err != nil {
		return nil, err
	}
	if err, ok := c.noPolicyAPI(status); ok {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("reading policy: %s: %s", http.StatusText(status), strings.TrimSpace(string(payload)))
	}
	var policy iamPolicy
	if err := json.Unmarshal(payload, &policy); err != nil {
		return nil, fmt.Errorf("decoding policy: %w", err)
	}
	return &policy, nil
}

func (c *iamClient) SetPolicy(ctx context.Context, resourceType, resourceID string, policy iamPolicy) (*iamPolicy, error) {
	payload, status, err := c.do(ctx, http.MethodPut, c.policyURL(resourceType, resourceID), setPolicyRequest{Policy: policy})
	if err != nil {
		return nil, err
	}
	if status == http.StatusConflict {
		return nil, errPolicyConflict
	}
	if err, ok := c.noPolicyAPI(status); ok {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("writing policy: %s: %s", http.StatusText(status), strings.TrimSpace(string(payload)))
	}
	var updated iamPolicy
	if err := json.Unmarshal(payload, &updated); err != nil {
		return nil, fmt.Errorf("decoding policy: %w", err)
	}
	return &updated, nil
}

// modifyPolicy applies a change as a read-modify-write against the current
// etag, retrying when another writer got there first.
//
// The _binding and _member resources exist so that several configurations can
// manage one resource, so conflicts are the normal path. One apply granting n
// members produces n writers racing for a single etag, and each conflict costs
// a writer its turn: the budget has to cover a queue draining one slot at a
// time.
func (c *iamClient) modifyPolicy(
	ctx context.Context,
	resourceType, resourceID string,
	modify func(*iamPolicy),
) error {
	const (
		attempts = 24
		baseWait = 25 * time.Millisecond
		maxWait  = 2 * time.Second
	)
	var lastErr error
	for attempt := range attempts {
		current, err := c.GetPolicy(ctx, resourceType, resourceID)
		if err != nil {
			return err
		}
		modify(current)
		if _, err := c.SetPolicy(ctx, resourceType, resourceID, *current); err != nil {
			if errors.Is(err, errPolicyConflict) {
				lastErr = err
				if err := sleepCtx(ctx, backoff(attempt, baseWait, maxWait)); err != nil {
					return err
				}
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("policy kept changing under concurrent writes after %d attempts: %w", attempts, lastErr)
}

// backoff draws a wait uniformly from [0, min(ceiling, base*2^attempt)). The
// jitter is what lets writers that collided at the same instant drain.
func backoff(attempt int, base, ceiling time.Duration) time.Duration {
	window := min(base<<min(attempt, 16), ceiling)
	return time.Duration(rand.Int64N(int64(window)))
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// bindingFor returns the members of one role, or nil when the role is unbound.
func (p *iamPolicy) bindingFor(role string) []string {
	for _, b := range p.Bindings {
		if b.Role == role {
			return b.Members
		}
	}
	return nil
}

// setRole replaces the members of one role, dropping the binding entirely when
// no members remain — an empty binding and an absent one mean the same thing,
// and keeping both representations would show a permanent diff.
func (p *iamPolicy) setRole(role string, members []string) {
	out := slices.DeleteFunc(slices.Clone(p.Bindings), func(b iamBinding) bool {
		return b.Role == role
	})
	if members = normaliseMembers(members); len(members) > 0 {
		out = append(out, iamBinding{Role: role, Members: members})
	}
	p.Bindings = out
}

func (p *iamPolicy) addMember(role, member string) {
	p.setRole(role, append(slices.Clone(p.bindingFor(role)), member))
}

func (p *iamPolicy) removeMember(role, member string) {
	p.setRole(role, slices.DeleteFunc(slices.Clone(p.bindingFor(role)), func(m string) bool {
		return m == member
	}))
}

// normaliseMembers sorts and de-duplicates so a reordered list in the
// configuration does not read as a change. Members themselves are left alone:
// trimming one would make state differ from configuration, which Terraform
// rejects.
func normaliseMembers(members []string) []string {
	out := slices.Clone(members)
	slices.Sort(out)
	return slices.Compact(out)
}

// sameBindings reports whether two documents grant the same thing. policy_data
// is a string, so without a semantic comparison any document not byte-identical
// to this provider's encoder — jsonencode output, say — plans an update on
// every run and never converges.
func sameBindings(a, b iamPolicy) bool {
	canonicalisePolicy(&a)
	canonicalisePolicy(&b)
	return slices.EqualFunc(a.Bindings, b.Bindings, func(x, y iamBinding) bool {
		return x.Role == y.Role && slices.Equal(x.Members, y.Members)
	})
}

// canonicalisePolicy puts a policy into the one shape both the data source and
// the policy resource encode: bindings sorted by role, members sorted and
// de-duplicated, empty bindings dropped. Without it the server's ordering and
// the configuration's read as a permanent diff.
func canonicalisePolicy(p *iamPolicy) {
	out := make([]iamBinding, 0, len(p.Bindings))
	for _, b := range p.Bindings {
		members := normaliseMembers(b.Members)
		if len(members) == 0 {
			continue
		}
		out = append(out, iamBinding{Role: b.Role, Members: members})
	}
	slices.SortFunc(out, func(x, y iamBinding) int { return cmp.Compare(x.Role, y.Role) })
	p.Bindings = out
}
