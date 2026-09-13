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
	"net/url"
	"strings"
	"time"

	marmot "github.com/marmotdata/marmot/sdk/go"
	"github.com/marmotdata/marmot/sdk/go/auth"
)

// secretStoreClient calls the secret-store endpoints, which the generated
// SDK does not cover, with the SDK client's host and credential.
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

// secretStore is a store as the server returns it, config normalised.
type secretStore struct {
	ID        string         `json:"id"`
	Name      string         `json:"name"`
	StoreType string         `json:"store_type"`
	Config    map[string]any `json:"config"`
	// Identity is set only when the store federates.
	Identity  *secretStoreIdentity `json:"identity,omitempty"`
	CreatedAt string               `json:"created_at"`
	UpdatedAt string               `json:"updated_at"`
}

// secretStoreIdentity is the OIDC identity a federated store presents to
// its backend.
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

// updateSecretStoreRequest replaces the config. The name is not sent; the
// resource replaces the store on a rename.
type updateSecretStoreRequest struct {
	Config map[string]any `json:"config"`
}

// secretStoreValidation is the store binary's verdict on the config.
type secretStoreValidation struct {
	Valid bool   `json:"valid"`
	Error string `json:"error,omitempty"`
}

// secretStoreSecret is a secret registered in a store. The ref comes back
// as sent.
type secretStoreSecret struct {
	ID            string         `json:"id"`
	SecretStoreID string         `json:"secret_store_id"`
	Ref           map[string]any `json:"ref"`
	CreatedAt     string         `json:"created_at"`
	UpdatedAt     string         `json:"updated_at"`
}

type secretRequest struct {
	Ref map[string]any `json:"ref"`
}

// pipelineSchedule is the SDK's schedule plus what the SDK drops: the
// secrets (config key to secret id) and the identity the pipeline
// presents when its plugin config federates.
type pipelineSchedule struct {
	marmot.Schedule
	Secrets  map[string]string `json:"secrets,omitempty"`
	Identity *pipelineIdentity `json:"identity,omitempty"`
}

// pipelineIdentity is what a cloud must trust for the pipeline: the
// issuer and the subject of the tokens Marmot mints for it. Absent on
// servers without a workload identity issuer.
type pipelineIdentity struct {
	Issuer  string `json:"issuer"`
	Subject string `json:"subject"`
}

// createScheduleRequest is what the SDK sends plus the secrets.
type createScheduleRequest struct {
	Name           string            `json:"name"`
	PluginID       string            `json:"plugin_id"`
	Config         map[string]any    `json:"config"`
	CronExpression string            `json:"cron_expression"`
	Enabled        bool              `json:"enabled"`
	Secrets        map[string]string `json:"secrets,omitempty"`
}

// updateScheduleRequest always sends Secrets: the server keeps what it has
// when the field is absent, and only an empty map clears it.
type updateScheduleRequest struct {
	Name           string            `json:"name"`
	PluginID       string            `json:"plugin_id"`
	Config         map[string]any    `json:"config"`
	CronExpression string            `json:"cron_expression"`
	Enabled        bool              `json:"enabled"`
	Secrets        map[string]string `json:"secrets"`
}

// errNotFound is a 404. Read treats it as the object having been removed.
var errNotFound = errors.New("not found")

// apiError is an error response. The server puts the message in
// {"error": "..."}.
type apiError struct {
	status  int
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%s: %s", http.StatusText(e.status), e.message)
}

// Is matches a 404 against errNotFound.
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

// errNoSecretStoreAPI means the instance has no secret-store endpoints. Only
// a request without an id in its path can tell: a 404 on POST /secret-stores
// is the route missing, while a 404 on /secret-stores/{id} may be the store
// gone.
type errNoSecretStoreAPI struct {
	host   string
	status int
}

func (e *errNoSecretStoreAPI) Error() string {
	return fmt.Sprintf("%s: no secret-store API (HTTP %d). Secret stores require "+
		"Marmot Cloud or Marmot Enterprise (https://cloud.marmotdata.io). Check the "+
		"provider's host, or remove the marmot_secret_store_* resources and any secrets "+
		"on marmot_pipeline", e.host, e.status)
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

// call performs a request and decodes the response into out on the expected
// status. Any other status is an *apiError.
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

// Ids come from configuration and import ids, so they are escaped.
func (c *secretStoreClient) storeURL(id string) string {
	return c.host + "/api/v1/secret-stores/" + url.PathEscape(id)
}

func (c *secretStoreClient) secretURL(storeID, id string) string {
	return c.storeURL(storeID) + "/secrets/" + url.PathEscape(id)
}

func (c *secretStoreClient) scheduleURL(id string) string {
	return c.host + "/api/v1/ingestion/schedules/" + url.PathEscape(id)
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

// DeleteSecretStore removes a store and its secrets. The server answers 409
// while a pipeline references one of them.
func (c *secretStoreClient) DeleteSecretStore(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, c.storeURL(id), nil, nil, http.StatusNoContent)
}

// ValidateSecretStore runs the store binary's Validate against the stored
// config. A rejected config is reported in the result, not as an error.
func (c *secretStoreClient) ValidateSecretStore(ctx context.Context, id string) (*secretStoreValidation, error) {
	var result secretStoreValidation
	if err := c.call(ctx, http.MethodPost, c.storeURL(id)+"/validate", nil, &result, http.StatusOK); err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateSecret registers a secret in a store. The server validates the ref
// against the store type.
func (c *secretStoreClient) CreateSecret(ctx context.Context, storeID string, ref map[string]any) (*secretStoreSecret, error) {
	var secret secretStoreSecret
	if err := c.call(ctx, http.MethodPost, c.storeURL(storeID)+"/secrets", secretRequest{Ref: ref}, &secret, http.StatusCreated); err != nil {
		return nil, err
	}
	return &secret, nil
}

func (c *secretStoreClient) GetSecret(ctx context.Context, storeID, id string) (*secretStoreSecret, error) {
	var secret secretStoreSecret
	if err := c.call(ctx, http.MethodGet, c.secretURL(storeID, id), nil, &secret, http.StatusOK); err != nil {
		return nil, err
	}
	return &secret, nil
}

// UpdateSecret repoints a secret in place.
func (c *secretStoreClient) UpdateSecret(ctx context.Context, storeID, id string, ref map[string]any) (*secretStoreSecret, error) {
	var secret secretStoreSecret
	if err := c.call(ctx, http.MethodPatch, c.secretURL(storeID, id), secretRequest{Ref: ref}, &secret, http.StatusOK); err != nil {
		return nil, err
	}
	return &secret, nil
}

// DeleteSecret removes a secret. The server answers 409 while a pipeline
// references it.
func (c *secretStoreClient) DeleteSecret(ctx context.Context, storeID, id string) error {
	return c.call(ctx, http.MethodDelete, c.secretURL(storeID, id), nil, nil, http.StatusNoContent)
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
		in.Secrets = map[string]string{}
	}
	var schedule pipelineSchedule
	if err := c.call(ctx, http.MethodPut, c.scheduleURL(id), in, &schedule, http.StatusOK); err != nil {
		return nil, err
	}
	return &schedule, nil
}
