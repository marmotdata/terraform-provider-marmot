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
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func secretKindNamed(t *testing.T, storeType string) secretKind {
	t.Helper()
	for _, kind := range secretKinds {
		if kind.storeType == storeType {
			return kind
		}
	}
	t.Fatalf("no %s secret kind", storeType)
	return secretKind{}
}

// secretFixture is one store type's secret resource and its tftypes shape.
type secretFixture struct {
	r       *secretResource
	schema  schema.Schema
	objType tftypes.Object
}

func newSecretFixture(t *testing.T, storeType string) secretFixture {
	t.Helper()
	r := &secretResource{kind: secretKindNamed(t, storeType)}
	s := schemaOf(t, r)
	objType, ok := s.Type().TerraformType(t.Context()).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is %T, want tftypes.Object", s.Type().TerraformType(t.Context()))
	}
	return secretFixture{r: r, schema: s, objType: objType}
}

func (f secretFixture) plan(t *testing.T, attrs map[string]tftypes.Value) tfsdk.Plan {
	t.Helper()
	return tfsdk.Plan{Schema: f.schema, Raw: objectOf(t, f.objType, attrs)}
}

func (f secretFixture) persist(t *testing.T, secret *secretStoreSecret) tfsdk.State {
	t.Helper()
	state := tfsdk.State{Schema: f.schema, Raw: objectOf(t, f.objType, nil)}
	var diags diag.Diagnostics
	f.r.persist(t.Context(), &state, secret, &diags)
	if diags.HasError() {
		t.Fatalf("persist: %v", diags)
	}
	return state
}

func num(n int64) tftypes.Value {
	return tftypes.NewValue(tftypes.Number, n)
}

// A default the server would apply is mirrored so it always goes on the
// wire; the server stores the ref as sent.
func TestSecretSchemaShape(t *testing.T) {
	defaulted := map[string]string{
		"google": "version",
		"aws":    "version_stage",
		"vault":  "mount",
	}
	for _, kind := range secretKinds {
		t.Run(kind.storeType, func(t *testing.T) {
			s := schemaOf(t, &secretResource{kind: kind})
			store, ok := s.Attributes["store"].(schema.StringAttribute)
			if !ok || !store.Required || len(store.PlanModifiers) == 0 {
				t.Errorf("store = %#v, want required with RequiresReplace", s.Attributes["store"])
			}
			if id, ok := s.Attributes["id"].(schema.StringAttribute); !ok || !id.Computed || id.Optional {
				t.Errorf("id = %#v, want computed-only", s.Attributes["id"])
			}
			for _, f := range kind.fields {
				if f.integer {
					if _, ok := s.Attributes[f.name].(schema.Int64Attribute); !ok {
						t.Errorf("%s = %#v, want an int64", f.name, s.Attributes[f.name])
					}
					continue
				}
				a, ok := s.Attributes[f.name].(schema.StringAttribute)
				if !ok {
					t.Fatalf("%s = %#v, want a string", f.name, s.Attributes[f.name])
				}
				if a.Required != f.required {
					t.Errorf("%s required = %v, want %v", f.name, a.Required, f.required)
				}
				if hasDefault := a.Default != nil; hasDefault != (defaulted[kind.storeType] == f.name) {
					t.Errorf("%s has default = %v", f.name, hasDefault)
				}
			}
		})
	}
}

// Only what is set goes on the wire; an integer attribute goes as a number.
func TestSecretRefSendsOnlyWhatIsSet(t *testing.T) {
	tests := []struct {
		name      string
		storeType string
		attrs     map[string]tftypes.Value
		want      map[string]any
	}{
		{
			name:      "google with default",
			storeType: "google",
			attrs: map[string]tftypes.Value{
				"store":     str("s1"),
				"project":   str("acme"),
				"secret_id": str("db-password"),
				"version":   str("latest"),
			},
			want: map[string]any{"project": "acme", "secret_id": "db-password", "version": "latest"},
		},
		{
			name:      "vault with version",
			storeType: "vault",
			attrs: map[string]tftypes.Value{
				"store":   str("s1"),
				"mount":   str("secret"),
				"name":    str("agents/analytics"),
				"version": num(2),
			},
			want: map[string]any{"mount": "secret", "name": "agents/analytics", "version": int64(2)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newSecretFixture(t, tt.storeType)
			var diags diag.Diagnostics
			got := f.r.refFrom(t.Context(), f.plan(t, tt.attrs), &diags)
			if diags.HasError() {
				t.Fatalf("refFrom: %v", diags)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ref = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// JSON numbers land in the integer attribute; a key not returned is null.
func TestSecretReadTakesTheRef(t *testing.T) {
	f := newSecretFixture(t, "vault")
	state := f.persist(t, &secretStoreSecret{
		ID: "sec1", SecretStoreID: "s1",
		Ref: map[string]any{"mount": "kv", "name": "agents/analytics", "version": float64(2)},
	})

	for name, want := range map[string]string{"id": "sec1", "store": "s1", "mount": "kv", "name": "agents/analytics"} {
		if got := stringAt(t, state, path.Root(name)); got.ValueString() != want {
			t.Errorf("%s = %v, want %q", name, got, want)
		}
	}
	var version types.Int64
	if diags := state.GetAttribute(t.Context(), path.Root("version"), &version); diags.HasError() {
		t.Fatalf("reading version: %v", diags)
	}
	if version.ValueInt64() != 2 {
		t.Errorf("version = %v, want 2", version)
	}
	if got := stringAt(t, state, path.Root("key")); !got.IsNull() {
		t.Errorf("key = %v, want null", got)
	}

	state = f.persist(t, &secretStoreSecret{ID: "sec1", SecretStoreID: "s1", Ref: map[string]any{"name": "db"}})
	if diags := state.GetAttribute(t.Context(), path.Root("version"), &version); diags.HasError() {
		t.Fatalf("reading version: %v", diags)
	}
	if !version.IsNull() {
		t.Errorf("version = %v, want null", version)
	}
}

func TestSecretImportID(t *testing.T) {
	f := newSecretFixture(t, "aws")

	tests := []struct {
		id      string
		wantErr bool
	}{
		{"s1/sec1", false},
		{"sec1", true},
		{"/sec1", true},
		{"s1/", true},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			resp := resource.ImportStateResponse{
				State: tfsdk.State{Schema: f.schema, Raw: tftypes.NewValue(f.objType, nil)},
			}
			f.r.ImportState(t.Context(), resource.ImportStateRequest{ID: tt.id}, &resp)
			if resp.Diagnostics.HasError() != tt.wantErr {
				t.Fatalf("diagnostics = %v, want error = %v", resp.Diagnostics, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if store := stringAt(t, resp.State, path.Root("store")); store.ValueString() != "s1" {
				t.Errorf("store = %v, want s1", store)
			}
			if id := stringAt(t, resp.State, path.Root("id")); id.ValueString() != "sec1" {
				t.Errorf("id = %v, want sec1", id)
			}
		})
	}
}
