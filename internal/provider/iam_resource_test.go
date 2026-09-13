// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func targetNamed(t *testing.T, typePrefix string) iamTarget {
	t.Helper()
	for _, target := range iamTargets {
		if target.typePrefix == typePrefix {
			return target
		}
	}
	t.Fatalf("no %s IAM target", typePrefix)
	return iamTarget{}
}

// iamFixture is the secret store target at one authority level with the
// tftypes shape a test needs to build plans and states for it.
type iamFixture struct {
	r       *iamResource
	schema  schema.Schema
	objType tftypes.Object
}

func newSecretStoreIAMFixture(t *testing.T, kind iamKind, client *iamClient) iamFixture {
	t.Helper()
	r := &iamResource{target: targetNamed(t, "secret_store"), kind: kind, client: client}
	s := schemaOf(t, r)
	objType, ok := s.Type().TerraformType(t.Context()).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is %T, want tftypes.Object", s.Type().TerraformType(t.Context()))
	}
	return iamFixture{r: r, schema: s, objType: objType}
}

func (f iamFixture) plan(t *testing.T, attrs map[string]tftypes.Value) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: f.schema, Raw: objectOf(t, f.objType, attrs)}
}

func (f iamFixture) state(t *testing.T, attrs map[string]tftypes.Value) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: f.schema, Raw: objectOf(t, f.objType, attrs)}
}

// Every target is crossed with the three authority levels, each type name
// once, and every schema passes the framework's own checks.
func TestIAMResourcesCoverEveryTargetAtEveryLevel(t *testing.T) {
	var names []string
	for _, newResource := range IAMResources() {
		r := newResource()
		name := typeName(t, r)
		if slices.Contains(names, name) {
			t.Fatalf("%s is registered twice", name)
		}
		names = append(names, name)
		if diags := schemaOf(t, r).ValidateImplementation(t.Context()); diags.HasError() {
			t.Errorf("%s: ValidateImplementation: %v", name, diags)
		}
	}
	for _, target := range iamTargets {
		for _, suffix := range []string{"_iam_policy", "_iam_binding", "_iam_member"} {
			if want := "marmot_" + target.typePrefix + suffix; !slices.Contains(names, want) {
				t.Errorf("%s is missing", want)
			}
		}
	}
}

// A store is addressed by its id, like every target below the root, and the
// API path uses the server's own spelling of the type.
func TestSecretStoreIAMTarget(t *testing.T) {
	target := targetNamed(t, "secret_store")
	if target.apiType != "secretStore" || target.idPrefix != "secret_store" || target.idAttr != "secret_store_id" {
		t.Errorf("target = %+v", target)
	}
	for _, kind := range []iamKind{iamKindPolicy, iamKindBinding, iamKindMember} {
		s := schemaOf(t, &iamResource{target: target, kind: kind})
		a, ok := s.Attributes["secret_store_id"].(schema.StringAttribute)
		if !ok || !a.Required || len(a.PlanModifiers) == 0 {
			t.Errorf("kind %d: secret_store_id = %#v, want required with RequiresReplace", kind, s.Attributes["secret_store_id"])
		}
	}
	c := newTestClient(t, nil)
	if got, want := c.policyURL(target.apiType, "s1"), c.host+"/api/v1/iam/secretStore/s1/policy"; got != want {
		t.Errorf("policyURL = %s, want %s", got, want)
	}
}

// A grant on a store goes to the store's policy, as one binding among any
// the store already has.
func TestSecretStoreIAMMemberWritesTheStorePolicy(t *testing.T) {
	var written iamPolicy
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/iam/secretStore/s1/policy" {
			t.Errorf("request to %s, want /api/v1/iam/secretStore/s1/policy", r.URL.Path)
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"etag":"e1","bindings":[{"role":"secretStore.user","members":["group:g1"]}]}`))
		case http.MethodPut:
			payload, err := io.ReadAll(r.Body)
			if err != nil {
				t.Errorf("reading request body: %v", err)
			}
			var req setPolicyRequest
			if err := json.Unmarshal(payload, &req); err != nil {
				t.Errorf("request body %q is not a policy: %v", payload, err)
			}
			written = req.Policy
			_, _ = w.Write([]byte(`{"etag":"e2","bindings":[]}`))
		}
	})
	f := newSecretStoreIAMFixture(t, iamKindMember, c)

	state := f.state(t, nil)
	var diags diag.Diagnostics
	f.r.apply(t.Context(), f.plan(t, map[string]tftypes.Value{
		"secret_store_id": str("s1"),
		"role":            str("roles/secretStore.reader"),
		"member":          str("serviceAccount:sa1"),
	}), &state, &diags)
	if diags.HasError() {
		t.Fatalf("apply: %v", diags)
	}

	if got := written.bindingFor("secretStore.reader"); !slices.Equal(got, []string{"serviceAccount:sa1"}) {
		t.Errorf("secretStore.reader = %v, want the service account", got)
	}
	if got := written.bindingFor("secretStore.user"); !slices.Equal(got, []string{"group:g1"}) {
		t.Errorf("secretStore.user = %v, want the existing grant kept", got)
	}
	if got, want := stringAt(t, state, path.Root("id")).ValueString(), "secret_store/s1/roles/secretStore.reader/serviceAccount:sa1"; got != want {
		t.Errorf("id = %s, want %s", got, want)
	}
	// The role goes back as configured: normalisation is for the wire only.
	if got := stringAt(t, state, path.Root("role")).ValueString(); got != "roles/secretStore.reader" {
		t.Errorf("role = %s, want it as configured", got)
	}
}

// A member the store's policy no longer carries is gone from state; one it
// does carry stays, with the etag it was read at.
func TestSecretStoreIAMMemberReadFollowsThePolicy(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		removed bool
	}{
		{"held", `{"etag":"e3","bindings":[{"role":"secretStore.reader","members":["serviceAccount:sa1"]}]}`, false},
		{"revoked", `{"etag":"e3","bindings":[{"role":"secretStore.reader","members":["serviceAccount:other"]}]}`, true},
		{"empty", `{"etag":"e3","bindings":[]}`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSecretStoreIAMFixture(t, iamKindMember, newTestClient(t, alwaysRespond(http.StatusOK, tt.body)))
			prior := f.state(t, map[string]tftypes.Value{
				"id":              str("secret_store/s1/roles/secretStore.reader/serviceAccount:sa1"),
				"etag":            str("e1"),
				"secret_store_id": str("s1"),
				"role":            str("secretStore.reader"),
				"member":          str("serviceAccount:sa1"),
			})

			var resp resource.ReadResponse
			resp.State = prior
			f.r.Read(t.Context(), resource.ReadRequest{State: prior}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("Read: %v", resp.Diagnostics)
			}
			if resp.State.Raw.IsNull() != tt.removed {
				t.Fatalf("removed = %v, want %v", resp.State.Raw.IsNull(), tt.removed)
			}
			if !tt.removed && stringAt(t, resp.State, path.Root("etag")).ValueString() != "e3" {
				t.Errorf("etag = %s, want e3", stringAt(t, resp.State, path.Root("etag")).ValueString())
			}
		})
	}
}

// An import id is the id this resource writes, so the two round trip.
func TestSecretStoreIAMImportRoundTrips(t *testing.T) {
	tests := []struct {
		kind iamKind
		id   string
		want map[string]string
	}{
		{iamKindPolicy, "secret_store/s1", map[string]string{"secret_store_id": "s1"}},
		{iamKindBinding, "secret_store/s1/roles/secretStore.reader", map[string]string{"secret_store_id": "s1", "role": "secretStore.reader"}},
		{iamKindMember, "secret_store/s1/roles/secretStore.reader/serviceAccount:sa1", map[string]string{"secret_store_id": "s1", "role": "secretStore.reader", "member": "serviceAccount:sa1"}},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			f := newSecretStoreIAMFixture(t, tt.kind, nil)
			var resp resource.ImportStateResponse
			resp.State = f.state(t, nil)
			f.r.ImportState(t.Context(), resource.ImportStateRequest{ID: tt.id}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("ImportState: %v", resp.Diagnostics)
			}
			for name, want := range tt.want {
				if got := stringAt(t, resp.State, path.Root(name)).ValueString(); got != want {
					t.Errorf("%s = %s, want %s", name, got, want)
				}
			}
			if got := f.r.stateID(tt.want["secret_store_id"], tt.want["role"], tt.want["member"]); got != tt.id {
				t.Errorf("stateID = %s, want %s", got, tt.id)
			}
		})
	}
}

// An id for another target is refused rather than read as a store id.
func TestSecretStoreIAMImportRejectsAnotherTarget(t *testing.T) {
	f := newSecretStoreIAMFixture(t, iamKindPolicy, nil)
	var resp resource.ImportStateResponse
	resp.State = f.state(t, nil)
	f.r.ImportState(t.Context(), resource.ImportStateRequest{ID: "asset/a1"}, &resp)
	if !resp.Diagnostics.HasError() {
		t.Fatal("imported an asset id into a secret store grant")
	}
	if !strings.Contains(resp.Diagnostics.Errors()[0].Detail(), "secret_store") {
		t.Errorf("error %q does not name the expected prefix", resp.Diagnostics.Errors()[0].Detail())
	}
}
