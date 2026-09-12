// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	marmot "github.com/marmotdata/marmot/sdk/go"
	"github.com/marmotdata/marmot/sdk/go/auth"
)

// secretStoreClient talks to the secret-store endpoints directly rather than
// through the generated SDK, which does not cover them: the stores themselves,
// the secrets registered on a pipeline, and service account leases. It reuses
// the SDK client's resolved host and credential so the provider keeps one
// authentication story.
type secretStoreClient struct {
	host string
	cred auth.Credential
	http *http.Client
}

func newSecretStoreClient(c *marmot.Client) *secretStoreClient {
	return &secretStoreClient{
		host: strings.TrimSuffix(c.Host(), "/"),
		cred: c.Credential(),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// sensitiveMask is what the server returns in place of a sensitive config
// value. Sending it back on an update keeps the stored value.
const sensitiveMask = "********"

// secretStore is a configured store: a type and how to reach its backend.
// Config comes back with sensitive fields masked and with the values the
// server derived or defaulted filled in.
type secretStore struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	StoreType string         `json:"store_type"`
	Config    map[string]any `json:"config"`
	// Identity is present only when the store uses federated auth.
	Identity  *secretStoreIdentity `json:"identity,omitempty"`
	CreatedAt string               `json:"created_at"`
	UpdatedAt string               `json:"updated_at"`
}

// secretStoreIdentity is the OIDC identity a federated store presents to
// its backend: what the customer trusts and grants on their side.
type secretStoreIdentity struct {
	Issuer   string `json:"issuer"`
	Subject  string `json:"subject"`
	Audience string `json:"audience"`
}

type createSecretStoreRequest struct {
	Name      string         `json:"name"`
	StoreType string         `json:"store_type"`
	Config    map[string]any `json:"config"`
}

// updateSecretStoreRequest replaces the stored config. The name is not
// sent: the server refuses to rename a federated store, and the resource
// replaces the store on a name change instead.
type updateSecretStoreRequest struct {
	Config map[string]any `json:"config"`
}

// secretStoreValidation is the store binary's verdict on the stored config.
type secretStoreValidation struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// pipelineSecret registers one secret on a pipeline: Ref is resolved from the
// store before each run and injected into the plugin config at Key.
type pipelineSecret struct {
	Key           string         `json:"key"`
	SecretStoreID string         `json:"secret_store_id"`
	Ref           map[string]any `json:"ref"`
}

// pipelineSchedule is the SDK's schedule plus the secrets the SDK drops.
type pipelineSchedule struct {
	marmot.Schedule
	Secrets []pipelineSecret `json:"secrets,omitempty"`
}

// createScheduleRequest carries the same fields the SDK sends plus the
// secrets. The server treats absent and empty secrets alike on create.
type createScheduleRequest struct {
	Name           string           `json:"name"`
	PluginID       string           `json:"plugin_id"`
	Config         map[string]any   `json:"config"`
	CronExpression string           `json:"cron_expression"`
	Enabled        bool             `json:"enabled"`
	Secrets        []pipelineSecret `json:"secrets,omitempty"`
}

// updateScheduleRequest always carries Secrets: the server keeps the
// registered secrets when the field is absent, and only an explicit empty
// list clears them. Callers pass an empty, non-nil slice for "none".
type updateScheduleRequest struct {
	Name           string           `json:"name"`
	PluginID       string           `json:"plugin_id"`
	Config         map[string]any   `json:"config"`
	CronExpression string           `json:"cron_expression"`
	Enabled        bool             `json:"enabled"`
	Secrets        []pipelineSecret `json:"secrets"`
}

// serviceAccountLease binds a service account to a location in a store where
// Marmot keeps a short-lived API key for it. The key itself is never returned.
type serviceAccountLease struct {
	ID               string         `json:"id"`
	ServiceAccountID string         `json:"service_account_id"`
	SecretStoreID    string         `json:"secret_store_id"`
	Ref              map[string]any `json:"ref"`
	TTLSeconds       int64          `json:"ttl_seconds"`
	CurrentKeyID     string         `json:"current_key_id,omitempty"`
	PreviousKeyID    string         `json:"previous_key_id,omitempty"`
	LeasedAt         string         `json:"leased_at,omitempty"`
	ExpiresAt        string         `json:"expires_at,omitempty"`
	LastError        string         `json:"last_error,omitempty"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

type setLeaseRequest struct {
	SecretStoreID string         `json:"secret_store_id"`
	Ref           map[string]any `json:"ref"`
	// TTLSeconds defaults to one hour on the server when zero.
	TTLSeconds int64 `json:"ttl_seconds,omitempty"`
}

// errNotFound means the server has nothing at the address asked for. A Read
// treats it as the object having been removed outside Terraform.
var errNotFound = errors.New("not found")

// apiError is a response the server answered with an error status. Every
// handled error carries its message as {"error": "..."}.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s: %s", http.StatusText(e.status), e.message)
}

// Is lets callers match a 404 with errors.Is(err, errNotFound).
func (e *apiError) Is(target error) bool {
	return target == errNotFound && e.status == http.StatusNotFound
}

func newAPIError(status int, payload []byte) *apiError {
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(payload, &body); err == nil && body.Error != "" {
		return &apiError{status: status, message: body.Error}
	}
	return &apiError{status: status, message: strings.TrimSpace(string(payload))}
}

// errNoSecretStoreAPI means the instance does not serve the secret-store
// endpoints. Only a request without an object id in its path can tell: a 404
// on POST /secret-stores is the route missing, whereas a 404 on
// /secret-stores/{id} may be the store gone, and the server answers both with
// the same body.
type errNoSecretStoreAPI struct {
	host   string
	status int
}

func (e *errNoSecretStoreAPI) Error() string {
	return fmt.Sprintf("%s: no secret-store API (HTTP %d). Secret stores require "+
		"Marmot Cloud or Marmot Enterprise (https://cloud.marmotdata.io). If this is not "+
		"the instance you meant to reach, check the provider's host setting; otherwise "+
		"remove the marmot_secret_store_* and marmot_service_account_lease resources and "+
		"any pipeline secret blocks from this configuration", e.host, e.status)
}

func (c *secretStoreClient) do(ctx context.Context, method, url string, body any) ([]byte, int, error) {
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

// call performs a request and decodes the response into out when the status
// is the one a success carries. Any other status is an *apiError.
func (c *secretStoreClient) call(ctx context.Context, method, url string, body, out any, want int) error {
	payload, status, err := c.do(ctx, method, url, body)
	if err != nil {
		return err
	}
	if status != want {
		return newAPIError(status, payload)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

func (c *secretStoreClient) storeURL(id string) string {
	return c.host + "/api/v1/secret-stores/" + id
}

func (c *secretStoreClient) scheduleURL(id string) string {
	return c.host + "/api/v1/ingestion/schedules/" + id
}

func (c *secretStoreClient) leaseURL(serviceAccountID string) string {
	return c.host + "/api/v1/service-accounts/" + serviceAccountID + "/lease"
}

func (c *secretStoreClient) CreateSecretStore(ctx context.Context, in createSecretStoreRequest) (*secretStore, error) {
	var store secretStore
	err := c.call(ctx, http.MethodPost, c.host+"/api/v1/secret-stores", in, &store, http.StatusCreated)
	var apiErr *apiError
	if errors.As(err, &apiErr) && (apiErr.status == http.StatusNotFound || apiErr.status == http.StatusNotImplemented) {
		return nil, &errNoSecretStoreAPI{host: c.host, status: apiErr.status}
	}
	if err != nil {
		return nil, err
	}
	return &store, nil
}

func (c *secretStoreClient) GetSecretStore(ctx context.Context, id string) (*secretStore, error) {
	var store secretStore
	if err := c.call(ctx, http.MethodGet, c.storeURL(id), nil, &store, http.StatusOK); err != nil {
		return nil, err
	}
	return &store, nil
}

func (c *secretStoreClient) UpdateSecretStore(ctx context.Context, id string, in updateSecretStoreRequest) (*secretStore, error) {
	var store secretStore
	if err := c.call(ctx, http.MethodPatch, c.storeURL(id), in, &store, http.StatusOK); err != nil {
		return nil, err
	}
	return &store, nil
}

// DeleteSecretStore removes a store. The server refuses with 409 while a
// pipeline secret or a lease still references it.
func (c *secretStoreClient) DeleteSecretStore(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, c.storeURL(id), nil, nil, http.StatusNoContent)
}

// ValidateSecretStore runs the store binary's Validate against the stored
// config. A config the binary rejects is reported in the result, not as an
// error.
func (c *secretStoreClient) ValidateSecretStore(ctx context.Context, id string) (*secretStoreValidation, error) {
	var result secretStoreValidation
	if err := c.call(ctx, http.MethodPost, c.storeURL(id)+"/validate", nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *secretStoreClient) CreateSchedule(ctx context.Context, in createScheduleRequest) (*pipelineSchedule, error) {
	var schedule pipelineSchedule
	if err := c.call(ctx, http.MethodPost, c.host+"/api/v1/ingestion/schedules", in, &schedule, http.StatusCreated); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (c *secretStoreClient) GetSchedule(ctx context.Context, id string) (*pipelineSchedule, error) {
	var schedule pipelineSchedule
	if err := c.call(ctx, http.MethodGet, c.scheduleURL(id), nil, &schedule, http.StatusOK); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (c *secretStoreClient) UpdateSchedule(ctx context.Context, id string, in updateScheduleRequest) (*pipelineSchedule, error) {
	if in.Secrets == nil {
		in.Secrets = []pipelineSecret{}
	}
	var schedule pipelineSchedule
	if err := c.call(ctx, http.MethodPut, c.scheduleURL(id), in, &schedule, http.StatusOK); err != nil {
		return nil, err
	}
	return &schedule, nil
}

func (c *secretStoreClient) GetLease(ctx context.Context, serviceAccountID string) (*serviceAccountLease, error) {
	var lease serviceAccountLease
	if err := c.call(ctx, http.MethodGet, c.leaseURL(serviceAccountID), nil, &lease, http.StatusOK); err != nil {
		return nil, err
	}
	return &lease, nil
}

// SetLease creates or repoints the account's one lease. The server performs
// the first renewal before answering and removes the lease again when that
// fails, so a ref the store cannot write to is a 400 here, naming the
// cause, rather than a lease that never renews.
func (c *secretStoreClient) SetLease(ctx context.Context, serviceAccountID string, in setLeaseRequest) (*serviceAccountLease, error) {
	var lease serviceAccountLease
	if err := c.call(ctx, http.MethodPut, c.leaseURL(serviceAccountID), in, &lease, http.StatusOK); err != nil {
		return nil, err
	}
	return &lease, nil
}

func (c *secretStoreClient) DeleteLease(ctx context.Context, serviceAccountID string) error {
	return c.call(ctx, http.MethodDelete, c.leaseURL(serviceAccountID), nil, nil, http.StatusNoContent)
}
