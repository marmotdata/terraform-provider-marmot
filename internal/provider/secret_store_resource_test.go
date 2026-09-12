// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// schemaOf builds a resource's schema, failing on any diagnostic.
func schemaOf(t *testing.T, r resource.Resource) schema.Schema {
	t.Helper()
	var resp resource.SchemaResponse
	r.Schema(t.Context(), resource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Schema: %v", resp.Diagnostics)
	}
	return resp.Schema
}

func typeName(t *testing.T, r resource.Resource) string {
	t.Helper()
	var resp resource.MetadataResponse
	r.Metadata(t.Context(), resource.MetadataRequest{ProviderTypeName: "marmot"}, &resp)
	return resp.TypeName
}

// The framework checks what the protocol will reject: a default on a
// required attribute, a computed block, a sensitive nested attribute in the
// wrong place. Catching that here beats catching it on the first plan.
func TestSecretStoreSchemasAreValid(t *testing.T) {
	resources := append(SecretStoreResources(),
		NewServiceAccountLeaseResource,
		NewPipelineResource,
	)
	seen := map[string]bool{}
	for _, newResource := range resources {
		r := newResource()
		name := typeName(t, r)
		t.Run(name, func(t *testing.T) {
			if seen[name] {
				t.Fatalf("%s is registered twice", name)
			}
			seen[name] = true
			if diags := schemaOf(t, r).ValidateImplementation(t.Context()); diags.HasError() {
				t.Fatalf("ValidateImplementation: %v", diags)
			}
		})
	}
}

// Every store exposes its identity the same way, and the auth keys the
// server fills in are computed so a plan can take the server's value.
func TestSecretStoreSchemaShape(t *testing.T) {
	derived := map[string][]string{
		"google": {"audience"},
		"aws":    {"audience", "session_name"},
		"azure":  {"audience"},
		"vault":  nil,
	}
	for _, kind := range secretStoreKinds {
		t.Run(kind.storeType, func(t *testing.T) {
			s := schemaOf(t, &secretStoreResource{kind: kind})
			for _, name := range []string{"issuer", "subject", "audience"} {
				a, ok := s.Attributes[name].(schema.StringAttribute)
				if !ok || !a.Computed || a.Optional || a.Required {
					t.Errorf("%s = %#v, want a computed-only string", name, s.Attributes[name])
				}
			}
			if a, ok := s.Attributes["name"].(schema.StringAttribute); !ok || len(a.PlanModifiers) == 0 {
				t.Errorf("name has no plan modifiers, want RequiresReplace")
			}
			auth, ok := s.Blocks["auth"].(schema.SingleNestedBlock)
			if !ok {
				t.Fatalf("auth = %#v, want a single nested block", s.Blocks["auth"])
			}
			for name, a := range auth.Attributes {
				str, ok := a.(schema.StringAttribute)
				if !ok {
					t.Fatalf("auth.%s = %#v, want a string", name, a)
				}
				wantDerived := false
				for _, d := range derived[kind.storeType] {
					wantDerived = wantDerived || d == name
				}
				isDerived := str.Computed && str.Default == nil
				if isDerived != wantDerived {
					t.Errorf("auth.%s computed without default = %v, want %v", name, isDerived, wantDerived)
				}
			}
		})
	}
}

func kindNamed(t *testing.T, storeType string) secretStoreKind {
	t.Helper()
	for _, kind := range secretStoreKinds {
		if kind.storeType == storeType {
			return kind
		}
	}
	t.Fatalf("no %s store kind", storeType)
	return secretStoreKind{}
}

// objectOf builds a fully populated object of typ, null for any attribute
// not given, which is how Terraform hands a plan or state over.
func objectOf(t *testing.T, typ tftypes.Object, attrs map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	values := make(map[string]tftypes.Value, len(typ.AttributeTypes))
	for name, attrType := range typ.AttributeTypes {
		if v, ok := attrs[name]; ok {
			values[name] = v
			continue
		}
		values[name] = tftypes.NewValue(attrType, nil)
	}
	return tftypes.NewValue(typ, values)
}

func str(s string) tftypes.Value {
	return tftypes.NewValue(tftypes.String, s)
}

var unknown = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)

// storeFixture is one store type's resource with the tftypes shapes a test
// needs to build plans and states for it.
type storeFixture struct {
	r        *secretStoreResource
	schema   schema.Schema
	objType  tftypes.Object
	authType tftypes.Object
}

func newStoreFixture(t *testing.T, storeType string) storeFixture {
	t.Helper()
	r := &secretStoreResource{kind: kindNamed(t, storeType)}
	s := schemaOf(t, r)
	objType, ok := s.Type().TerraformType(t.Context()).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is %T, want tftypes.Object", s.Type().TerraformType(t.Context()))
	}
	authType, ok := objType.AttributeTypes["auth"].(tftypes.Object)
	if !ok {
		t.Fatalf("auth type is %T, want tftypes.Object", objType.AttributeTypes["auth"])
	}
	return storeFixture{r: r, schema: s, objType: objType, authType: authType}
}

func (f storeFixture) auth(t *testing.T, attrs map[string]tftypes.Value) tftypes.Value {
	t.Helper()
	return objectOf(t, f.authType, attrs)
}

func (f storeFixture) plan(t *testing.T, attrs map[string]tftypes.Value) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: f.schema, Raw: objectOf(t, f.objType, attrs)}
}

func (f storeFixture) state(t *testing.T, attrs map[string]tftypes.Value) tfsdk.State {
	t.Helper()
	return tfsdk.State{Schema: f.schema, Raw: objectOf(t, f.objType, attrs)}
}

// nullState is the prior state of a resource being created.
func (f storeFixture) nullState() tfsdk.State {
	return tfsdk.State{Schema: f.schema, Raw: tftypes.NewValue(f.objType, nil)}
}

// persist runs persist against a fresh state and returns it.
func (f storeFixture) persist(t *testing.T, prior attrGetter, store *secretStore) tfsdk.State {
	t.Helper()
	state := f.state(t, nil)
	var diags diag.Diagnostics
	f.r.persist(t.Context(), &state, prior, store, &diags)
	if diags.HasError() {
		t.Fatalf("persist: %v", diags)
	}
	return state
}

func stringAt(t *testing.T, state tfsdk.State, p path.Path) types.String {
	t.Helper()
	var v types.String
	if diags := state.GetAttribute(t.Context(), p, &v); diags.HasError() {
		t.Fatalf("reading %s: %v", p, diags)
	}
	return v
}

func authOf(t *testing.T, state tfsdk.State) types.Object {
	t.Helper()
	var auth types.Object
	if diags := state.GetAttribute(t.Context(), path.Root("auth"), &auth); diags.HasError() {
		t.Fatalf("reading auth: %v", diags)
	}
	return auth
}

// Only what is set and known goes on the wire, so the server applies its own
// defaults and derivations and the config reads back with the keys written.
func TestSecretStoreConfigSendsOnlyWhatIsSet(t *testing.T) {
	tests := []struct {
		name      string
		storeType string
		attrs     func(f storeFixture) map[string]tftypes.Value
		want      map[string]any
	}{
		{
			name:      "auth block",
			storeType: "vault",
			attrs: func(f storeFixture) map[string]tftypes.Value {
				return map[string]tftypes.Value{
					"name":    str("vault-prod"),
					"address": str("https://vault.acme.internal"),
					"ca_cert": str("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"),
					"auth": f.auth(t, map[string]tftypes.Value{
						"method": str("token"),
						"token":  str("s3cr3t"),
					}),
				}
			},
			want: map[string]any{
				"address": "https://vault.acme.internal",
				"ca_cert": "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
				"auth":    map[string]any{"method": "token", "token": "s3cr3t"},
			},
		},
		{
			name:      "no auth block",
			storeType: "vault",
			attrs: func(storeFixture) map[string]tftypes.Value {
				return map[string]tftypes.Value{
					"name":      str("vault-prod"),
					"address":   str("https://vault.acme.internal"),
					"namespace": str("platform"),
				}
			},
			want: map[string]any{
				"address":   "https://vault.acme.internal",
				"namespace": "platform",
			},
		},
		{
			name:      "derived value left for the server",
			storeType: "google",
			attrs: func(f storeFixture) map[string]tftypes.Value {
				return map[string]tftypes.Value{
					"name":    str("gcp-prod"),
					"project": str("acme"),
					"auth": f.auth(t, map[string]tftypes.Value{
						"method":                     str("federated"),
						"workload_identity_provider": str("projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot"),
						"audience":                   unknown,
					}),
				}
			},
			want: map[string]any{
				"project": "acme",
				"auth": map[string]any{
					"method":                     "federated",
					"workload_identity_provider": "projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStoreFixture(t, tt.storeType)
			var diags diag.Diagnostics
			got := f.r.configFrom(t.Context(), f.plan(t, tt.attrs(f)), &diags)
			if diags.HasError() {
				t.Fatalf("configFrom: %v", diags)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("config = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// The server never returns a sensitive value, only a mask. State keeps what
// was written, and the mask never lands in it.
func TestSecretStoreReadKeepsTheSensitiveValue(t *testing.T) {
	f := newStoreFixture(t, "vault")

	prior := f.state(t, map[string]tftypes.Value{
		"id":      str("s1"),
		"name":    str("vault-prod"),
		"address": str("https://vault.acme.internal"),
		"auth": f.auth(t, map[string]tftypes.Value{
			"method": str("token"),
			"token":  str("s3cr3t"),
		}),
	})
	state := f.persist(t, prior, &secretStore{
		ID: "s1", Name: "vault-prod", StoreType: "vault",
		Config: map[string]any{
			"address": "https://vault.acme.internal",
			"auth":    map[string]any{"method": "token", "token": sensitiveMask},
		},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
	})

	auth := authOf(t, state)
	if got := objectString(auth, "token"); got.ValueString() != "s3cr3t" {
		t.Errorf("token = %v, want the prior value kept", got)
	}
	if got := objectString(auth, "method"); got.ValueString() != "token" {
		t.Errorf("method = %v, want token", got)
	}
	if got := objectString(auth, "role"); !got.IsNull() {
		t.Errorf("role = %v, want null for a key the server did not return", got)
	}
	if got := stringAt(t, state, path.Root("updated_at")); got.ValueString() != "2026-01-02T00:00:00Z" {
		t.Errorf("updated_at = %v", got)
	}
}

// A store written without an auth block reads back without one, whether the
// server echoes nothing or the defaults it applied. A store whose method is
// not the default was set up elsewhere, and an import shows it.
func TestSecretStoreReadWithoutAuthBlock(t *testing.T) {
	tests := []struct {
		name     string
		config   map[string]any
		wantNull bool
	}{
		{"no auth returned", map[string]any{"address": "https://vault.acme.internal"}, true},
		{"server defaults returned", map[string]any{
			"address": "https://vault.acme.internal",
			"auth":    map[string]any{"method": "kubernetes", "mount_path": "kubernetes"},
		}, true},
		{"another method returned", map[string]any{
			"address": "https://vault.acme.internal",
			"auth":    map[string]any{"method": "federated", "role": "marmot"},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStoreFixture(t, "vault")
			state := f.persist(t, f.state(t, nil), &secretStore{
				ID: "s1", Name: "vault-prod", StoreType: "vault", Config: tt.config,
			})
			if auth := authOf(t, state); auth.IsNull() != tt.wantNull {
				t.Errorf("auth = %v, want null = %v", auth, tt.wantNull)
			}
		})
	}
}

// A derived value the server filled in lands in state next to what was
// written, so a plan that keeps it sees no drift.
func TestSecretStoreReadTakesDerivedValues(t *testing.T) {
	f := newStoreFixture(t, "aws")

	prior := f.plan(t, map[string]tftypes.Value{
		"name": str("aws-prod"),
		"auth": f.auth(t, map[string]tftypes.Value{
			"method":       str("federated"),
			"role_arn":     str("arn:aws:iam::123456789012:role/marmot"),
			"audience":     unknown,
			"session_name": unknown,
		}),
	})
	state := f.persist(t, prior, &secretStore{
		ID: "s1", Name: "aws-prod", StoreType: "aws",
		Config: map[string]any{
			"auth": map[string]any{
				"method": "federated", "role_arn": "arn:aws:iam::123456789012:role/marmot",
				"audience": "sts.amazonaws.com", "session_name": "marmot",
			},
		},
	})

	auth := authOf(t, state)
	if got := objectString(auth, "audience"); got.ValueString() != "sts.amazonaws.com" {
		t.Errorf("audience = %v, want the server's default", got)
	}
	if got := objectString(auth, "session_name"); got.ValueString() != "marmot" {
		t.Errorf("session_name = %v, want the server's default", got)
	}
}

// A federated store's identity is what the user binds on the cloud side, so
// it is exposed as is; a store without one reads as null.
func TestSecretStoreReadExposesTheIdentity(t *testing.T) {
	f := newStoreFixture(t, "google")

	tests := []struct {
		name     string
		identity *secretStoreIdentity
	}{
		{"federated", &secretStoreIdentity{
			Issuer:   "https://acme.cloud.marmotdata.io",
			Subject:  "secretStore:gcp-prod",
			Audience: "//iam.googleapis.com/projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot",
		}},
		{"default credentials", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := f.persist(t, f.state(t, nil), &secretStore{
				ID: "s1", Name: "gcp-prod", StoreType: "google", Identity: tt.identity,
				Config: map[string]any{"project": "acme"},
			})
			issuer := stringAt(t, state, path.Root("issuer"))
			subject := stringAt(t, state, path.Root("subject"))
			audience := stringAt(t, state, path.Root("audience"))
			if tt.identity == nil {
				if !issuer.IsNull() || !subject.IsNull() || !audience.IsNull() {
					t.Errorf("identity = %v %v %v, want all null", issuer, subject, audience)
				}
				return
			}
			if issuer.ValueString() != tt.identity.Issuer || subject.ValueString() != tt.identity.Subject || audience.ValueString() != tt.identity.Audience {
				t.Errorf("identity = %v %v %v, want %+v", issuer, subject, audience, tt.identity)
			}
		})
	}
}

// A value the server derives is planned from state while what it derives
// from stands still, and left to the server otherwise.
func TestKeepStateUnless(t *testing.T) {
	f := newStoreFixture(t, "google")
	provider := "projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot"
	derived := "//iam.googleapis.com/" + provider

	stored := map[string]tftypes.Value{
		"id":      str("s1"),
		"name":    str("gcp-prod"),
		"subject": str("secretStore:gcp-prod"),
		"auth": f.auth(t, map[string]tftypes.Value{
			"method":                     str("federated"),
			"workload_identity_provider": str(provider),
			"audience":                   str(derived),
		}),
	}
	planned := func(auth map[string]tftypes.Value) map[string]tftypes.Value {
		return map[string]tftypes.Value{
			"id":      str("s1"),
			"name":    str("gcp-prod"),
			"subject": unknown,
			"auth":    f.auth(t, auth),
		}
	}

	tests := []struct {
		name  string
		attr  path.Path
		state tfsdk.State
		plan  tfsdk.Plan
		want  types.String
	}{
		{
			name:  "inputs unchanged",
			attr:  path.Root("auth").AtName("audience"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"method":                     str("federated"),
				"workload_identity_provider": str(provider),
				"audience":                   unknown,
				"service_account":            str("sa@acme.iam.gserviceaccount.com"),
			})),
			want: types.StringValue(derived),
		},
		{
			name:  "input changed",
			attr:  path.Root("auth").AtName("audience"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"method":                     str("federated"),
				"workload_identity_provider": str("projects/123/locations/global/workloadIdentityPools/marmot/providers/other"),
				"audience":                   unknown,
			})),
			want: types.StringUnknown(),
		},
		{
			name:  "input unknown",
			attr:  path.Root("auth").AtName("audience"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"method":                     str("federated"),
				"workload_identity_provider": unknown,
				"audience":                   unknown,
			})),
			want: types.StringUnknown(),
		},
		{
			name:  "create",
			attr:  path.Root("auth").AtName("audience"),
			state: f.nullState(),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"method":                     str("federated"),
				"workload_identity_provider": str(provider),
				"audience":                   unknown,
			})),
			want: types.StringUnknown(),
		},
		{
			name:  "identity follows the auth block",
			attr:  path.Root("subject"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"method":                     str("federated"),
				"workload_identity_provider": str(provider),
				"audience":                   str(derived),
			})),
			want: types.StringValue("secretStore:gcp-prod"),
		},
		{
			name:  "identity changes with the auth block",
			attr:  path.Root("subject"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"method": str("default"),
			})),
			want: types.StringUnknown(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stateValue, planValue types.String
			if !tt.state.Raw.IsNull() {
				if diags := tt.state.GetAttribute(t.Context(), tt.attr, &stateValue); diags.HasError() {
					t.Fatalf("state %s: %v", tt.attr, diags)
				}
			}
			if diags := tt.plan.GetAttribute(t.Context(), tt.attr, &planValue); diags.HasError() {
				t.Fatalf("plan %s: %v", tt.attr, diags)
			}
			a, ok := tt.plan.Schema.(schema.Schema)
			if !ok {
				t.Fatalf("schema is %T", tt.plan.Schema)
			}
			attribute, diags := a.AttributeAtPath(t.Context(), tt.attr)
			if diags.HasError() {
				t.Fatalf("attribute %s: %v", tt.attr, diags)
			}
			stringAttr, ok := attribute.(schema.StringAttribute)
			if !ok {
				t.Fatalf("attribute %s is %T, want a string attribute", tt.attr, attribute)
			}
			modifiers := stringAttr.PlanModifiers
			if len(modifiers) != 1 {
				t.Fatalf("%s has %d plan modifiers, want one", tt.attr, len(modifiers))
			}

			resp := planmodifier.StringResponse{PlanValue: planValue}
			modifiers[0].PlanModifyString(t.Context(), planmodifier.StringRequest{
				Path:       tt.attr,
				Plan:       tt.plan,
				PlanValue:  planValue,
				State:      tt.state,
				StateValue: stateValue,
			}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("PlanModifyString: %v", resp.Diagnostics)
			}
			if !resp.PlanValue.Equal(tt.want) {
				t.Errorf("planned %v, want %v", resp.PlanValue, tt.want)
			}
		})
	}
}

// Blocks round-trip through the API shape, and "no blocks" is an empty set
// rather than a null one, matching how Terraform sends an absent block.
func TestPipelineSecretsRoundTrip(t *testing.T) {
	secrets := []pipelineSecret{
		{Key: "password", SecretStoreID: "s1", Ref: map[string]any{"secret": "db-password", "version": "latest"}},
		{Key: "credentials.private_key", SecretStoreID: "s2", Ref: map[string]any{"path": "agents", "version": float64(2)}},
	}

	set, diags := secretBlocks(t.Context(), secrets)
	if diags.HasError() {
		t.Fatalf("secretBlocks: %v", diags)
	}
	if set.IsNull() || len(set.Elements()) != 2 {
		t.Fatalf("set = %v, want two elements", set)
	}

	back, diags := scheduleSecrets(t.Context(), set)
	if diags.HasError() {
		t.Fatalf("scheduleSecrets: %v", diags)
	}
	if !reflect.DeepEqual(back, secrets) {
		t.Errorf("round trip = %#v, want %#v", back, secrets)
	}

	empty, diags := secretBlocks(t.Context(), nil)
	if diags.HasError() {
		t.Fatalf("secretBlocks(nil): %v", diags)
	}
	if empty.IsNull() || len(empty.Elements()) != 0 {
		t.Errorf("no secrets = %v, want an empty set", empty)
	}
	none, diags := scheduleSecrets(t.Context(), types.SetNull(pipelineSecretType))
	if diags.HasError() {
		t.Fatalf("scheduleSecrets(null): %v", diags)
	}
	if none == nil || len(none) != 0 {
		t.Errorf("null set = %#v, want an empty, non-nil list", none)
	}
}

func TestLeaseRequestCarriesTheRef(t *testing.T) {
	in, diags := leaseRequest(&ServiceAccountLeaseResourceModel{
		Store:      types.StringValue("s1"),
		Ref:        jsontypes.NewNormalizedValue(`{"path": "agents/analytics", "key": "api_key"}`),
		TTLSeconds: types.Int64Value(1800),
	})
	if diags.HasError() {
		t.Fatalf("leaseRequest: %v", diags)
	}
	want := setLeaseRequest{
		SecretStoreID: "s1",
		Ref:           map[string]any{"path": "agents/analytics", "key": "api_key"},
		TTLSeconds:    1800,
	}
	if !reflect.DeepEqual(in, want) {
		t.Errorf("request = %#v, want %#v", in, want)
	}
}
