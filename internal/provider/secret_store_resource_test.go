// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"reflect"
	"testing"

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

// The framework rejects what the protocol would, such as a default on a
// required attribute.
func TestSecretStoreSchemasAreValid(t *testing.T) {
	resources := append(SecretStoreResources(), SecretStoreSecretResources()...)
	resources = append(resources,
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

// Config keys the server fills in are computed: a constant default is
// mirrored, a derived one follows its inputs.
func TestSecretStoreSchemaShape(t *testing.T) {
	defaulted := map[string][]string{
		"aws":   {"session_name"},
		"vault": {"auth_path"},
	}
	for _, kind := range secretStoreKinds {
		t.Run(kind.storeType, func(t *testing.T) {
			s := schemaOf(t, &secretStoreResource{kind: kind})
			if len(s.Blocks) != 0 {
				t.Errorf("blocks = %v, want none", s.Blocks)
			}
			for _, name := range []string{"issuer", "subject"} {
				a, ok := s.Attributes[name].(schema.StringAttribute)
				if !ok || !a.Computed || a.Optional || a.Required {
					t.Errorf("%s = %#v, want a computed-only string", name, s.Attributes[name])
				}
			}
			if a, ok := s.Attributes["name"].(schema.StringAttribute); !ok || len(a.PlanModifiers) == 0 {
				t.Errorf("name has no plan modifiers, want RequiresReplace")
			}
			for _, name := range kind.federation {
				a, ok := s.Attributes[name].(schema.StringAttribute)
				if !ok || !a.Optional || a.Computed {
					t.Errorf("%s = %#v, want an optional string", name, s.Attributes[name])
				}
			}
			for _, f := range kind.fields {
				a, ok := s.Attributes[f.name].(schema.StringAttribute)
				if !ok {
					t.Fatalf("%s = %#v, want a string", f.name, s.Attributes[f.name])
				}
				wantDefault := false
				for _, d := range defaulted[kind.storeType] {
					wantDefault = wantDefault || d == f.name
				}
				if hasDefault := a.Default != nil; hasDefault != wantDefault {
					t.Errorf("%s has default = %v, want %v", f.name, hasDefault, wantDefault)
				}
				if f.name == "audience" && (!a.Optional || !a.Computed || a.Default != nil || len(a.PlanModifiers) != 1) {
					t.Errorf("audience = %#v, want optional, computed, derived", a)
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

// objectOf builds an object of typ, null for any attribute not given.
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

// storeFixture is one store type's resource and its tftypes shape.
type storeFixture struct {
	r       *secretStoreResource
	schema  schema.Schema
	objType tftypes.Object
}

func newStoreFixture(t *testing.T, storeType string) storeFixture {
	t.Helper()
	r := &secretStoreResource{kind: kindNamed(t, storeType)}
	s := schemaOf(t, r)
	objType, ok := s.Type().TerraformType(t.Context()).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is %T, want tftypes.Object", s.Type().TerraformType(t.Context()))
	}
	return storeFixture{r: r, schema: s, objType: objType}
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
func (f storeFixture) persist(t *testing.T, store *secretStore) tfsdk.State {
	t.Helper()
	state := f.state(t, nil)
	var diags diag.Diagnostics
	f.r.persist(t.Context(), &state, store, &diags)
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

// Only what is set and known goes on the wire.
func TestSecretStoreConfigSendsOnlyWhatIsSet(t *testing.T) {
	tests := []struct {
		name      string
		storeType string
		attrs     map[string]tftypes.Value
		want      map[string]any
	}{
		{
			name:      "federated vault",
			storeType: "vault",
			attrs: map[string]tftypes.Value{
				"name":      str("vault-prod"),
				"address":   str("https://vault.acme.internal"),
				"ca_cert":   str("-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"),
				"role":      str("marmot"),
				"auth_path": str("jwt"),
				"audience":  unknown,
			},
			want: map[string]any{
				"address":   "https://vault.acme.internal",
				"ca_cert":   "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
				"role":      "marmot",
				"auth_path": "jwt",
			},
		},
		{
			name:      "own credentials",
			storeType: "google",
			attrs: map[string]tftypes.Value{
				"name":            str("gcp-prod"),
				"service_account": str("sa@acme.iam.gserviceaccount.com"),
			},
			want: map[string]any{
				"service_account": "sa@acme.iam.gserviceaccount.com",
			},
		},
		{
			name:      "derived value left for the server",
			storeType: "google",
			attrs: map[string]tftypes.Value{
				"name":                       str("gcp-prod"),
				"workload_identity_provider": str("projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot"),
				"audience":                   unknown,
			},
			want: map[string]any{
				"workload_identity_provider": "projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newStoreFixture(t, tt.storeType)
			var diags diag.Diagnostics
			got := f.r.configFrom(t.Context(), f.plan(t, tt.attrs), &diags)
			if diags.HasError() {
				t.Fatalf("configFrom: %v", diags)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("config = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// Server-filled values land in state; a key the server did not return is
// null.
func TestSecretStoreReadTakesTheServersConfig(t *testing.T) {
	f := newStoreFixture(t, "aws")

	state := f.persist(t, &secretStore{
		ID: "s1", Name: "aws-prod", StoreType: "aws",
		Config: map[string]any{
			"role_arn":     "arn:aws:iam::123456789012:role/marmot",
			"audience":     "sts.amazonaws.com",
			"session_name": "marmot",
		},
		CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-02T00:00:00Z",
	})

	for name, want := range map[string]string{
		"role_arn":     "arn:aws:iam::123456789012:role/marmot",
		"audience":     "sts.amazonaws.com",
		"session_name": "marmot",
		"updated_at":   "2026-01-02T00:00:00Z",
	} {
		if got := stringAt(t, state, path.Root(name)); got.ValueString() != want {
			t.Errorf("%s = %v, want %q", name, got, want)
		}
	}

	f = newStoreFixture(t, "vault")
	state = f.persist(t, &secretStore{
		ID: "s1", Name: "vault-prod", StoreType: "vault",
		Config: map[string]any{"address": "https://vault.acme.internal", "auth_path": "jwt"},
	})
	for _, name := range []string{"role", "audience", "namespace"} {
		if got := stringAt(t, state, path.Root(name)); !got.IsNull() {
			t.Errorf("%s = %v, want null for a key the server did not return", name, got)
		}
	}
}

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
		{"own credentials", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := f.persist(t, &secretStore{
				ID: "s1", Name: "gcp-prod", StoreType: "google", Identity: tt.identity,
			})
			issuer := stringAt(t, state, path.Root("issuer"))
			subject := stringAt(t, state, path.Root("subject"))
			if tt.identity == nil {
				if !issuer.IsNull() || !subject.IsNull() {
					t.Errorf("identity = %v %v, want both null", issuer, subject)
				}
				return
			}
			if issuer.ValueString() != tt.identity.Issuer || subject.ValueString() != tt.identity.Subject {
				t.Errorf("identity = %v %v, want %+v", issuer, subject, tt.identity)
			}
		})
	}
}

// A derived value is planned from state while its inputs stand still.
func TestKeepStateUnless(t *testing.T) {
	f := newStoreFixture(t, "google")
	provider := "projects/123/locations/global/workloadIdentityPools/marmot/providers/marmot"
	derived := "//iam.googleapis.com/" + provider

	stored := map[string]tftypes.Value{
		"id":                         str("s1"),
		"name":                       str("gcp-prod"),
		"subject":                    str("secretStore:gcp-prod"),
		"workload_identity_provider": str(provider),
		"audience":                   str(derived),
	}
	planned := func(attrs map[string]tftypes.Value) map[string]tftypes.Value {
		out := map[string]tftypes.Value{
			"id":      str("s1"),
			"name":    str("gcp-prod"),
			"subject": unknown,
		}
		for k, v := range attrs {
			out[k] = v
		}
		return out
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
			attr:  path.Root("audience"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"workload_identity_provider": str(provider),
				"audience":                   unknown,
				"service_account":            str("sa@acme.iam.gserviceaccount.com"),
			})),
			want: types.StringValue(derived),
		},
		{
			name:  "input changed",
			attr:  path.Root("audience"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"workload_identity_provider": str("projects/123/locations/global/workloadIdentityPools/marmot/providers/other"),
				"audience":                   unknown,
			})),
			want: types.StringUnknown(),
		},
		{
			name:  "input unknown",
			attr:  path.Root("audience"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"workload_identity_provider": unknown,
				"audience":                   unknown,
			})),
			want: types.StringUnknown(),
		},
		{
			name:  "create",
			attr:  path.Root("audience"),
			state: f.nullState(),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"workload_identity_provider": str(provider),
				"audience":                   unknown,
			})),
			want: types.StringUnknown(),
		},
		{
			name:  "identity follows the federation",
			attr:  path.Root("subject"),
			state: f.state(t, stored),
			plan: f.plan(t, planned(map[string]tftypes.Value{
				"workload_identity_provider": str(provider),
				"audience":                   str(derived),
			})),
			want: types.StringValue("secretStore:gcp-prod"),
		},
		{
			name:  "identity changes with the federation",
			attr:  path.Root("subject"),
			state: f.state(t, stored),
			plan:  f.plan(t, planned(nil)),
			want:  types.StringUnknown(),
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

// "No secrets" goes as an empty map and reads back as whatever was written,
// null or `{}`, since the API omits the field for both.
func TestPipelineSecretsRoundTrip(t *testing.T) {
	secrets := map[string]string{"password": "sec1", "credentials.private_key": "sec2"}

	m, diags := secretsValue(t.Context(), secrets, types.MapNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("secretsValue: %v", diags)
	}
	if m.IsNull() || len(m.Elements()) != 2 {
		t.Fatalf("map = %v, want two elements", m)
	}

	back, diags := scheduleSecrets(t.Context(), m)
	if diags.HasError() {
		t.Fatalf("scheduleSecrets: %v", diags)
	}
	if !reflect.DeepEqual(back, secrets) {
		t.Errorf("round trip = %#v, want %#v", back, secrets)
	}

	none, diags := scheduleSecrets(t.Context(), types.MapNull(types.StringType))
	if diags.HasError() {
		t.Fatalf("scheduleSecrets(null): %v", diags)
	}
	if none == nil || len(none) != 0 {
		t.Errorf("null map = %#v, want an empty, non-nil map", none)
	}

	empty := types.MapValueMust(types.StringType, nil)
	for _, prior := range []types.Map{types.MapNull(types.StringType), empty} {
		got, diags := secretsValue(t.Context(), nil, prior)
		if diags.HasError() {
			t.Fatalf("secretsValue(nil): %v", diags)
		}
		if !got.Equal(prior) {
			t.Errorf("no secrets with prior %v = %v, want the prior kept", prior, got)
		}
	}
}
