// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
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
var errPolicyConflict = fmt.Errorf("iam policy changed since it was read")

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
// The non-authoritative resources (_binding, _member) exist precisely so that
// several Terraform configurations can manage different roles on one resource.
// That guarantees concurrent read-modify-write cycles, so retrying a conflict
// is the normal path, not an error case.
func (c *iamClient) modifyPolicy(
	ctx context.Context,
	resourceType, resourceID string,
	modify func(*iamPolicy),
) error {
	const attempts = 5
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		current, err := c.GetPolicy(ctx, resourceType, resourceID)
		if err != nil {
			return err
		}
		modify(current)
		if _, err := c.SetPolicy(ctx, resourceType, resourceID, *current); err != nil {
			if err == errPolicyConflict {
				lastErr = err
				time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
				continue
			}
			return err
		}
		return nil
	}
	return fmt.Errorf("policy kept changing under concurrent writes: %w", lastErr)
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
	members = normaliseMembers(members)
	out := make([]iamBinding, 0, len(p.Bindings)+1)
	replaced := false
	for _, b := range p.Bindings {
		if b.Role != role {
			out = append(out, b)
			continue
		}
		replaced = true
		if len(members) > 0 {
			out = append(out, iamBinding{Role: role, Members: members})
		}
	}
	if !replaced && len(members) > 0 {
		out = append(out, iamBinding{Role: role, Members: members})
	}
	p.Bindings = out
}

func (p *iamPolicy) addMember(role, member string) {
	members := append(append([]string{}, p.bindingFor(role)...), member)
	p.setRole(role, members)
}

func (p *iamPolicy) removeMember(role, member string) {
	current := p.bindingFor(role)
	out := make([]string, 0, len(current))
	for _, m := range current {
		if m != member {
			out = append(out, m)
		}
	}
	p.setRole(role, out)
}

// normaliseMembers sorts and de-duplicates so a reordered list in the
// configuration does not read as a change.
func normaliseMembers(members []string) []string {
	seen := make(map[string]struct{}, len(members))
	out := make([]string, 0, len(members))
	for _, m := range members {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

// canonicalisePolicy puts a policy into the one shape both the data source
// and the policy resource encode: bindings sorted by role, members sorted and
// de-duplicated, empty bindings dropped. Without a single canonical form the
// server's ordering (by member type, then id) and the configuration's
// ordering read as a permanent diff.
func canonicalisePolicy(p *iamPolicy) {
	out := make([]iamBinding, 0, len(p.Bindings))
	for _, b := range p.Bindings {
		members := normaliseMembers(b.Members)
		if len(members) == 0 {
			continue
		}
		out = append(out, iamBinding{Role: b.Role, Members: members})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Role < out[j].Role })
	p.Bindings = out
}
