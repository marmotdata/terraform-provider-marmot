// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/marmotdata/marmot/sdk/go/auth"
)

// newTestSecretStoreClient points a secretStoreClient at a stub server.
func newTestSecretStoreClient(t *testing.T, handler http.HandlerFunc) *secretStoreClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &secretStoreClient{host: srv.URL, cred: auth.APIKey("test"), http: srv.Client()}
}

// recordedRequest is what the stub server saw last.
type recordedRequest struct {
	method string
	path   string
	apiKey string
	body   string
}

// record answers every request with status and body, keeping the last
// request for the test to inspect.
func record(t *testing.T, status int, body string) (http.HandlerFunc, *recordedRequest) {
	t.Helper()
	var last recordedRequest
	return func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
		}
		last = recordedRequest{
			method: r.Method,
			path:   r.URL.Path,
			apiKey: r.Header.Get("X-API-Key"),
			body:   string(payload),
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}, &last
}

// bodyOf decodes a recorded JSON body.
func bodyOf(t *testing.T, r *recordedRequest) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal([]byte(r.body), &out); err != nil {
		t.Fatalf("request body %q is not JSON: %v", r.body, err)
	}
	return out
}

func TestCreateSecretStoreSendsTypeAndConfig(t *testing.T) {
	handler, last := record(t, http.StatusCreated, `{
		"id": "s1", "name": "gcp-prod", "store_type": "google",
		"config": {"project": "acme", "auth": {"method": "default"}},
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"
	}`)
	c := newTestSecretStoreClient(t, handler)

	store, err := c.CreateSecretStore(t.Context(), createSecretStoreRequest{
		Name:      "gcp-prod",
		StoreType: "google",
		Config:    map[string]any{"project": "acme", "auth": map[string]any{"method": "default"}},
	})
	if err != nil {
		t.Fatalf("CreateSecretStore: %v", err)
	}
	if last.method != http.MethodPost || last.path != "/api/v1/secret-stores" {
		t.Errorf("sent %s %s, want POST /api/v1/secret-stores", last.method, last.path)
	}
	if last.apiKey != "test" {
		t.Errorf("X-API-Key = %q, want the credential", last.apiKey)
	}
	body := bodyOf(t, last)
	if body["store_type"] != "google" || body["name"] != "gcp-prod" {
		t.Errorf("body = %v, want store_type google and name gcp-prod", body)
	}
	if store.ID != "s1" || store.StoreType != "google" || store.Config["project"] != "acme" {
		t.Errorf("decoded %+v", store)
	}
}

// A POST carries no id, so a 404 there can only be the route missing. Marmot
// Cloud answers 501 when a feature is off; open-source answers 404 with the
// same body a missing store would, and reporting it as "Not Found" would read
// as though the store had vanished.
func TestCreateSecretStoreReportsAMissingAPI(t *testing.T) {
	for _, status := range []int{http.StatusNotImplemented, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := newTestSecretStoreClient(t, alwaysRespond(status, `{"error":"Not found"}`))

			var unsupported *errNoSecretStoreAPI
			_, err := c.CreateSecretStore(t.Context(), createSecretStoreRequest{Name: "x", StoreType: "google"})
			if !errors.As(err, &unsupported) {
				t.Fatalf("got %v, want *errNoSecretStoreAPI", err)
			}
			for _, want := range []string{c.host, strconv.Itoa(status)} {
				if !strings.Contains(unsupported.Error(), want) {
					t.Errorf("error %q does not mention %q", unsupported.Error(), want)
				}
			}
			if errors.Is(err, errNotFound) {
				t.Error("a missing API must not read as a missing store")
			}
		})
	}
}

// The server explains a rejected config in the body; that text is what the
// user needs to see.
func TestCreateSecretStoreSurfacesTheServerMessage(t *testing.T) {
	tests := []struct {
		status int
		body   string
		want   string
	}{
		{http.StatusBadRequest, `{"error":"invalid input: address is required"}`, "address is required"},
		{http.StatusConflict, `{"error":"A secret store with this name already exists"}`, "already exists"},
		{http.StatusBadGateway, `upstream down`, "upstream down"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			c := newTestSecretStoreClient(t, alwaysRespond(tt.status, tt.body))
			_, err := c.CreateSecretStore(t.Context(), createSecretStoreRequest{Name: "x", StoreType: "vault"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, tt.want)
			}
		})
	}
}

// A 404 on a store's own URL is the store being gone, which Read turns into
// removing it from state.
func TestGetSecretStoreReportsNotFound(t *testing.T) {
	c := newTestSecretStoreClient(t, alwaysRespond(http.StatusNotFound, `{"error":"Secret store not found"}`))
	_, err := c.GetSecretStore(t.Context(), "s1")
	if !errors.Is(err, errNotFound) {
		t.Fatalf("got %v, want errNotFound", err)
	}
	if !strings.Contains(err.Error(), "Secret store not found") {
		t.Errorf("error %q lost the server's message", err)
	}
}

// The update carries the config and nothing else: an empty config still
// goes, so the server replaces what it has, and no name goes, since the
// server refuses to rename a federated store and the resource replaces the
// store on a rename instead.
func TestUpdateSecretStoreSendsTheConfig(t *testing.T) {
	tests := []struct {
		name string
		in   updateSecretStoreRequest
		want string
	}{
		{"empty config", updateSecretStoreRequest{Config: map[string]any{}}, `{"config":{}}`},
		{"config", updateSecretStoreRequest{Config: map[string]any{"address": "x"}}, `{"config":{"address":"x"}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, last := record(t, http.StatusOK, `{"id":"s1","name":"vault-prod","store_type":"vault","config":{}}`)
			c := newTestSecretStoreClient(t, handler)

			if _, err := c.UpdateSecretStore(t.Context(), "s1", tt.in); err != nil {
				t.Fatalf("UpdateSecretStore: %v", err)
			}
			if last.method != http.MethodPatch || last.path != "/api/v1/secret-stores/s1" {
				t.Errorf("sent %s %s, want PATCH /api/v1/secret-stores/s1", last.method, last.path)
			}
			if last.body != tt.want {
				t.Errorf("body = %s, want %s", last.body, tt.want)
			}
		})
	}
}

// A federated store carries the identity the user binds on the cloud side;
// a store on default credentials carries none.
func TestGetSecretStoreDecodesTheIdentity(t *testing.T) {
	tests := []struct {
		name string
		body string
		want *secretStoreIdentity
	}{
		{"federated", `{
			"id": "s1", "name": "gcp-prod", "store_type": "google",
			"config": {"project": "acme", "auth": {"method": "federated", "workload_identity_provider": "projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot", "audience": "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot"}},
			"identity": {"issuer": "https://acme.cloud.marmotdata.io", "subject": "secretStore:gcp-prod", "audience": "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot"}
		}`, &secretStoreIdentity{
			Issuer:   "https://acme.cloud.marmotdata.io",
			Subject:  "secretStore:gcp-prod",
			Audience: "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot",
		}},
		{"default credentials", `{
			"id": "s1", "name": "gcp-prod", "store_type": "google",
			"config": {"project": "acme", "auth": {"method": "default"}}
		}`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestSecretStoreClient(t, alwaysRespond(http.StatusOK, tt.body))
			store, err := c.GetSecretStore(t.Context(), "s1")
			if err != nil {
				t.Fatalf("GetSecretStore: %v", err)
			}
			if !reflect.DeepEqual(store.Identity, tt.want) {
				t.Errorf("identity = %+v, want %+v", store.Identity, tt.want)
			}
		})
	}
}

func TestDeleteSecretStore(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     string
		notFound bool
		wantErr  string
	}{
		{"deleted", http.StatusNoContent, "", false, ""},
		{"already gone", http.StatusNotFound, `{"error":"Secret store not found"}`, true, "Secret store not found"},
		{"still referenced", http.StatusConflict, `{"error":"Secret store is referenced by a pipeline or a service account lease"}`, false, "referenced"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, last := record(t, tt.status, tt.body)
			c := newTestSecretStoreClient(t, handler)

			err := c.DeleteSecretStore(t.Context(), "s1")
			if last.method != http.MethodDelete || last.path != "/api/v1/secret-stores/s1" {
				t.Errorf("sent %s %s, want DELETE /api/v1/secret-stores/s1", last.method, last.path)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("got %v, want success", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("got %v, want an error mentioning %q", err, tt.wantErr)
			}
			if errors.Is(err, errNotFound) != tt.notFound {
				t.Errorf("errors.Is(errNotFound) = %v, want %v", !tt.notFound, tt.notFound)
			}
		})
	}
}

// A config the binary rejects is a verdict, not a failed request.
func TestValidateSecretStoreReportsTheVerdict(t *testing.T) {
	handler, last := record(t, http.StatusOK, `{"valid":false,"error":"store vault-prod: dial tcp: refused"}`)
	c := newTestSecretStoreClient(t, handler)

	result, err := c.ValidateSecretStore(t.Context(), "s1")
	if err != nil {
		t.Fatalf("ValidateSecretStore: %v", err)
	}
	if last.method != http.MethodPost || last.path != "/api/v1/secret-stores/s1/validate" {
		t.Errorf("sent %s %s, want POST /api/v1/secret-stores/s1/validate", last.method, last.path)
	}
	if result.Valid || !strings.Contains(result.Error, "refused") {
		t.Errorf("got %+v, want invalid with the binary's reason", result)
	}
}

// On create the server treats absent and empty secrets alike, so an empty
// list is left out of the body.
func TestCreateScheduleSendsSecretsOnlyWhenThereAreAny(t *testing.T) {
	tests := []struct {
		name    string
		secrets []pipelineSecret
		want    bool
	}{
		{"none", []pipelineSecret{}, false},
		{"one", []pipelineSecret{{Key: "password", SecretStoreID: "s1", Ref: map[string]any{"secret": "db"}}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, last := record(t, http.StatusCreated, `{"id":"p1","name":"orders","enabled":true}`)
			c := newTestSecretStoreClient(t, handler)

			schedule, err := c.CreateSchedule(t.Context(), createScheduleRequest{
				Name: "orders", PluginID: "postgresql", CronExpression: "0 * * * *", Enabled: true,
				Config: map[string]any{"host": "db"}, Secrets: tt.secrets,
			})
			if err != nil {
				t.Fatalf("CreateSchedule: %v", err)
			}
			if last.method != http.MethodPost || last.path != "/api/v1/ingestion/schedules" {
				t.Errorf("sent %s %s, want POST /api/v1/ingestion/schedules", last.method, last.path)
			}
			_, sent := bodyOf(t, last)["secrets"]
			if sent != tt.want {
				t.Errorf("body %s: secrets present = %v, want %v", last.body, sent, tt.want)
			}
			if schedule.ID != "p1" || !schedule.Enabled {
				t.Errorf("decoded %+v, want the SDK fields filled", schedule.Schedule)
			}
		})
	}
}

// On update an absent list keeps the registered secrets, so "none" has to go
// on the wire as an empty list or a removed block would never clear them.
func TestUpdateScheduleAlwaysSendsSecrets(t *testing.T) {
	tests := []struct {
		name    string
		secrets []pipelineSecret
		want    string
	}{
		{"nil", nil, `"secrets":[]`},
		{"empty", []pipelineSecret{}, `"secrets":[]`},
		{"one", []pipelineSecret{{Key: "password", SecretStoreID: "s1", Ref: map[string]any{"secret": "db"}}}, `"secrets":[{"key":"password","secret_store_id":"s1","ref":{"secret":"db"}}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, last := record(t, http.StatusOK, `{"id":"p1","name":"orders"}`)
			c := newTestSecretStoreClient(t, handler)

			if _, err := c.UpdateSchedule(t.Context(), "p1", updateScheduleRequest{Name: "orders", Secrets: tt.secrets}); err != nil {
				t.Fatalf("UpdateSchedule: %v", err)
			}
			if last.method != http.MethodPut || last.path != "/api/v1/ingestion/schedules/p1" {
				t.Errorf("sent %s %s, want PUT /api/v1/ingestion/schedules/p1", last.method, last.path)
			}
			if !strings.Contains(last.body, tt.want) {
				t.Errorf("body %s does not contain %s", last.body, tt.want)
			}
		})
	}
}

// The schedule decodes into the SDK's shape, so the existing mapping keeps
// working, with the secrets alongside. Refs carry non-string values.
func TestGetScheduleDecodesSecretsAlongsideTheSchedule(t *testing.T) {
	c := newTestSecretStoreClient(t, alwaysRespond(http.StatusOK, `{
		"id": "p1", "name": "orders", "plugin_id": "postgresql",
		"config": {"host": "db"}, "cron_expression": "0 * * * *", "enabled": true,
		"last_run_status": "success", "created_at": "2026-01-01T00:00:00Z",
		"secrets": [{"key": "password", "secret_store_id": "s1", "ref": {"path": "db", "version": 2}}]
	}`))

	schedule, err := c.GetSchedule(t.Context(), "p1")
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if schedule.ID != "p1" || schedule.LastRunStatus != "success" || schedule.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Errorf("SDK fields: %+v", schedule.Schedule)
	}
	config, ok := schedule.Config.(map[string]any)
	if !ok || config["host"] != "db" {
		t.Errorf("config = %#v, want the object", schedule.Config)
	}
	if len(schedule.Secrets) != 1 {
		t.Fatalf("secrets = %+v, want one", schedule.Secrets)
	}
	secret := schedule.Secrets[0]
	if secret.Key != "password" || secret.SecretStoreID != "s1" || secret.Ref["version"] != float64(2) {
		t.Errorf("secret = %+v", secret)
	}
}

func TestGetScheduleReportsNotFound(t *testing.T) {
	c := newTestSecretStoreClient(t, alwaysRespond(http.StatusNotFound, `{"error":"Schedule not found"}`))
	if _, err := c.GetSchedule(t.Context(), "p1"); !errors.Is(err, errNotFound) {
		t.Fatalf("got %v, want errNotFound", err)
	}
}

func TestSetLeaseSendsTheLeaseAndDecodesItsStatus(t *testing.T) {
	handler, last := record(t, http.StatusOK, `{
		"id": "l1", "service_account_id": "sa1", "secret_store_id": "s1",
		"ref": {"path": "agents/analytics", "key": "api_key"}, "ttl_seconds": 3600,
		"current_key_id": "k2", "previous_key_id": "k1",
		"leased_at": "2026-01-01T00:00:00Z", "expires_at": "2026-01-01T01:00:00Z",
		"created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"
	}`)
	c := newTestSecretStoreClient(t, handler)

	lease, err := c.SetLease(t.Context(), "sa1", setLeaseRequest{
		SecretStoreID: "s1",
		Ref:           map[string]any{"path": "agents/analytics", "key": "api_key"},
		TTLSeconds:    3600,
	})
	if err != nil {
		t.Fatalf("SetLease: %v", err)
	}
	if last.method != http.MethodPut || last.path != "/api/v1/service-accounts/sa1/lease" {
		t.Errorf("sent %s %s, want PUT /api/v1/service-accounts/sa1/lease", last.method, last.path)
	}
	body := bodyOf(t, last)
	if body["secret_store_id"] != "s1" || body["ttl_seconds"] != float64(3600) {
		t.Errorf("body = %v", body)
	}
	if lease.ID != "l1" || lease.CurrentKeyID != "k2" || lease.PreviousKeyID != "k1" || lease.ExpiresAt != "2026-01-01T01:00:00Z" || lease.LastError != "" {
		t.Errorf("decoded %+v", lease)
	}
}

// The server's refusal is what the user needs to see: the permission it
// lacks, or why the first write to the store failed. The latter also
// removes the lease, and must not read as one that was never there.
func TestSetLeaseSurfacesTheServerMessage(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{"forbidden", http.StatusForbidden, `{"error":"Leasing through a secret store requires secretStore:use"}`, "secretStore:use"},
		{"first renewal failed", http.StatusBadRequest, `{"error":"invalid input: first renewal: writing to secret store vault-prod: permission denied on agents/analytics"}`, "first renewal: writing to secret store vault-prod: permission denied"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestSecretStoreClient(t, alwaysRespond(tt.status, tt.body))
			_, err := c.SetLease(t.Context(), "sa1", setLeaseRequest{SecretStoreID: "s1"})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("got %v, want an error mentioning %q", err, tt.want)
			}
			if errors.Is(err, errNotFound) {
				t.Error("a refused lease must not read as a missing one")
			}
		})
	}
}

func TestLeaseNotFound(t *testing.T) {
	c := newTestSecretStoreClient(t, alwaysRespond(http.StatusNotFound, `{"error":"Lease not found"}`))
	if _, err := c.GetLease(t.Context(), "sa1"); !errors.Is(err, errNotFound) {
		t.Fatalf("GetLease: got %v, want errNotFound", err)
	}
	if err := c.DeleteLease(t.Context(), "sa1"); !errors.Is(err, errNotFound) {
		t.Fatalf("DeleteLease: got %v, want errNotFound", err)
	}
}

func TestDeleteLease(t *testing.T) {
	handler, last := record(t, http.StatusNoContent, "")
	c := newTestSecretStoreClient(t, handler)

	if err := c.DeleteLease(t.Context(), "sa1"); err != nil {
		t.Fatalf("DeleteLease: %v", err)
	}
	if last.method != http.MethodDelete || last.path != "/api/v1/service-accounts/sa1/lease" {
		t.Errorf("sent %s %s, want DELETE /api/v1/service-accounts/sa1/lease", last.method, last.path)
	}
}
