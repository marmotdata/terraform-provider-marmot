// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// secretStoreField is one key of a store's config, exposed as an attribute of
// the same name. The provider sends the keys that are set and reads back the
// config as the server validated and stored it, defaults and derived values
// included.
type secretStoreField struct {
	name        string
	description string
	required    bool
	// serverDefault is a constant the server applies when the key is unset,
	// mirrored as the attribute's default so the plan knows it.
	serverDefault string
	// derivedFrom marks a value the server fills in when the key is unset,
	// from the named sibling keys. The plan keeps the stored value until one
	// of those changes.
	derivedFrom []string
}

func (f secretStoreField) attribute() schema.Attribute {
	a := schema.StringAttribute{
		MarkdownDescription: f.description,
		Required:            f.required,
		Optional:            !f.required,
	}
	if f.serverDefault != "" {
		a.MarkdownDescription += " Defaults to `" + f.serverDefault + "`."
		a.Computed = true
		a.Default = stringdefault.StaticString(f.serverDefault)
	}
	if len(f.derivedFrom) > 0 {
		a.Computed = true
		a.PlanModifiers = []planmodifier.String{rootPaths(f.derivedFrom...)}
	}
	return a
}

// keepStateUnless plans the stored value of a computed attribute the server
// fills in, as long as none of the attributes it is derived from change. A
// changed or unknown input leaves it unknown for the server to set again.
type keepStateUnless []path.Path

var _ planmodifier.String = keepStateUnless(nil)

func rootPaths(names ...string) keepStateUnless {
	out := make(keepStateUnless, 0, len(names))
	for _, name := range names {
		out = append(out, path.Root(name))
	}
	return out
}

func (m keepStateUnless) Description(ctx context.Context) string {
	return m.MarkdownDescription(ctx)
}

func (m keepStateUnless) MarkdownDescription(context.Context) string {
	return "Keeps the stored value unless an attribute it is derived from changes."
}

func (m keepStateUnless) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.PlanValue.IsUnknown() || req.State.Raw.IsNull() {
		return
	}
	for _, p := range m {
		before, ok := valueAt(req.State.Raw, p)
		if !ok {
			return
		}
		after, ok := valueAt(req.Plan.Raw, p)
		if !ok || !after.IsFullyKnown() || !after.Equal(before) {
			return
		}
	}
	resp.PlanValue = req.StateValue
}

// valueAt reads the value at an attribute path of a raw plan or state. A
// path into a null or unknown object has no value.
func valueAt(root tftypes.Value, p path.Path) (tftypes.Value, bool) {
	tfPath := tftypes.NewAttributePath()
	for _, step := range p.Steps() {
		name, ok := step.(path.PathStepAttributeName)
		if !ok {
			return tftypes.Value{}, false
		}
		tfPath = tfPath.WithAttributeName(string(name))
	}
	v, _, err := tftypes.WalkAttributePath(root, tfPath)
	if err != nil {
		return tftypes.Value{}, false
	}
	value, ok := v.(tftypes.Value)
	return value, ok
}

// secretStoreKind describes one store type as a Terraform resource. Every
// type shares the CRUD below over the generic secret-store API; only the
// schema differs.
type secretStoreKind struct {
	// storeType is the server's id for the type, and the resource name suffix.
	storeType   string
	label       string
	description string
	// federation names the keys that, when set, make the store federate.
	// The store's identity follows them.
	federation []string
	fields     []secretStoreField
}

func audienceField(description string, derivedFrom ...string) secretStoreField {
	return secretStoreField{
		name: "audience",
		description: "Audience the Marmot token carries, which the backend must expect. " +
			description + " Only meaningful on a federated store.",
		derivedFrom: derivedFrom,
	}
}

var secretStoreKinds = []secretStoreKind{
	{
		storeType: "google",
		label:     "Google Secret Manager",
		description: "A Google Secret Manager store. Without `workload_identity_provider` the " +
			"server reads with its own Application Default Credentials; with it, Marmot presents " +
			"an OIDC token for the subject `secretStore:{name}` that the provider exchanges for a " +
			"credential granted only what this store may reach.",
		federation: []string{"workload_identity_provider"},
		fields: []secretStoreField{
			{
				name: "workload_identity_provider",
				description: "Workload Identity Federation provider the Marmot token is exchanged at, " +
					"`projects/{number}/locations/global/workloadIdentityPools/{pool}/providers/{provider}` " +
					"(the `name` of a `google_iam_workload_identity_pool_provider`). Setting it makes " +
					"the store federate.",
			},
			{
				name: "service_account",
				description: "Service account email to impersonate after federation. Empty " +
					"accesses Secret Manager directly as the federated principal.",
			},
			audienceField("Derived from `workload_identity_provider` by the server "+
				"(`https://iam.googleapis.com/{provider}`) when unset.", "workload_identity_provider"),
		},
	},
	{
		storeType: "aws",
		label:     "AWS Secrets Manager",
		description: "An AWS Secrets Manager store. Without `role_arn` the server reads with its " +
			"own credential chain (IRSA in a pod); with it, Marmot presents an OIDC token for the " +
			"subject `secretStore:{name}` and assumes the role.",
		federation: []string{"role_arn"},
		fields: []secretStoreField{
			{
				name: "role_arn",
				description: "IAM role to assume with the Marmot token. Its trust policy must name " +
					"the Marmot issuer as an OIDC provider. Setting it makes the store federate.",
			},
			{
				name:          "session_name",
				description:   "Session name of the assumed role, visible in CloudTrail.",
				serverDefault: "marmot",
			},
			audienceField("The OIDC provider's client ID, which the role's trust policy "+
				"conditions `aud` on; the server sets `sts.amazonaws.com` when unset.", "role_arn"),
		},
	},
	{
		storeType: "azure",
		label:     "Azure Key Vault",
		description: "An Azure Key Vault store. Without `tenant_id` and `client_id` the server " +
			"reads with its own `DefaultAzureCredential` (a managed identity in a pod); with them, " +
			"Marmot presents an OIDC token for the subject `secretStore:{name}` to the app " +
			"registration's federated credential.",
		federation: []string{"tenant_id", "client_id"},
		fields: []secretStoreField{
			{
				name: "tenant_id",
				description: "Entra tenant of the app registration the Marmot token is exchanged " +
					"for. Set together with `client_id` to make the store federate.",
			},
			{
				name: "client_id",
				description: "App registration (client) ID carrying a federated credential that " +
					"trusts the Marmot issuer. Set together with `tenant_id`.",
			},
			audienceField("Matches the federated credential's audience; the server sets "+
				"`api://AzureADTokenExchange` when unset.", "tenant_id", "client_id"),
		},
	},
	{
		storeType: "vault",
		label:     "HashiCorp Vault",
		description: "A HashiCorp Vault KV v2 store. Without `role` the server logs in with the " +
			"token in its own `VAULT_TOKEN`; with it, Marmot presents an OIDC token for the subject " +
			"`secretStore:{name}` to the JWT auth method.",
		federation: []string{"role"},
		fields: []secretStoreField{
			{
				name:        "address",
				description: "Vault server URL, stored without a trailing slash.",
				required:    true,
			},
			{name: "namespace", description: "Vault Enterprise namespace, sent as `X-Vault-Namespace`."},
			{
				name: "ca_cert",
				description: "PEM-encoded CA certificate to verify the Vault server's TLS certificate " +
					"against. Omit to use the system roots.",
			},
			{
				name: "role",
				description: "Role of the JWT auth method to log in as with the Marmot token. " +
					"Setting it makes the store federate.",
			},
			{
				name:          "auth_path",
				description:   "Mount path of the JWT auth method the Marmot token is presented to.",
				serverDefault: "jwt",
			},
			audienceField("The JWT role's `bound_audiences`; the server sets the Vault "+
				"address when unset.", "role", "address"),
		},
	},
}

// SecretStoreResources returns one resource per store type.
func SecretStoreResources() []func() resource.Resource {
	out := make([]func() resource.Resource, 0, len(secretStoreKinds))
	for _, kind := range secretStoreKinds {
		out = append(out, func() resource.Resource {
			return &secretStoreResource{kind: kind}
		})
	}
	return out
}

type secretStoreResource struct {
	kind   secretStoreKind
	client *secretStoreClient
}

var _ resource.Resource = &secretStoreResource{}
var _ resource.ResourceWithImportState = &secretStoreResource{}

// secretStoreCloudOnly heads every store page. Configuring the provider makes
// no request, and a resource being created is not read beforehand, so without
// this the first clue is a failed apply.
const secretStoreCloudOnly = "~> **Requires Marmot Cloud or Marmot Enterprise.** Open-source Marmot " +
	"serves no secret-store API, so this resource fails on apply rather than at plan. " +
	"[Marmot Cloud](https://cloud.marmotdata.io) includes it on every plan, Free included.\n\n"

func (r *secretStoreResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_secret_store_" + r.kind.storeType
}

func (r *secretStoreResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data",
			fmt.Sprintf("Expected *marmot.Client, got %T", req.ProviderData))
		return
	}
	r.client = newSecretStoreClient(client)
}

func (r *secretStoreResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	// The identity follows the name and whether the store federates.
	identityInputs := rootPaths(append([]string{"name"}, r.kind.federation...)...)

	attrs := map[string]schema.Attribute{
		"name": schema.StringAttribute{
			MarkdownDescription: "Name of the store, unique per instance. On a federated store it is " +
				"also the store's identity: the token subject is `secretStore:{name}`. Changing it " +
				"replaces the store, since bindings on the old subject would stop matching.",
			Required: true,
			Validators: []validator.String{
				stringvalidator.LengthBetween(1, 255),
			},
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.RequiresReplace(),
			},
		},
		"id": schema.StringAttribute{
			MarkdownDescription: "Secret store ID",
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"subject": schema.StringAttribute{
			MarkdownDescription: "Subject of the tokens the store presents, `secretStore:{name}`: the " +
				"value to grant on the cloud side, such as the subject of a `principal://` member on " +
				"Google Cloud or the `sub` condition of an AWS role trust policy. Null unless the " +
				"store federates.",
			Computed:      true,
			PlanModifiers: []planmodifier.String{identityInputs},
		},
		"issuer": schema.StringAttribute{
			MarkdownDescription: "Issuer URL of the tokens the store presents: the OIDC provider to " +
				"register with the cloud identity provider, once per account. Null unless the " +
				"store federates.",
			Computed:      true,
			PlanModifiers: []planmodifier.String{identityInputs},
		},
		"created_at": schema.StringAttribute{
			MarkdownDescription: "Creation timestamp",
			Computed:            true,
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"updated_at": schema.StringAttribute{
			MarkdownDescription: "Last update timestamp",
			Computed:            true,
		},
	}
	for _, f := range r.kind.fields {
		attrs[f.name] = f.attribute()
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: secretStoreCloudOnly + r.kind.description +
			"\n\nA store holds no secret values. `marmot_secret_store_" + r.kind.storeType + "_secret` " +
			"registers where a secret lives in it; a `marmot_pipeline` reads such a secret into " +
			"its config before each run, and a `marmot_service_account_lease` writes short-lived " +
			"keys to one.\n\n" +
			"A federated store has an OIDC identity of its own: `issuer`, `subject` and `audience` " +
			"are what to trust and grant on the cloud side, so the store reaches only the secrets " +
			"bound to it.",
		Attributes: attrs,
	}
}

// configFrom assembles the config the API takes from the planned attributes.
// Null and unknown attributes are left out so the server applies its own
// defaults and derivations; a derived value the plan kept from state goes
// back as is, which the server accepts since it is what it would derive.
func (r *secretStoreResource) configFrom(ctx context.Context, src attrGetter, diags *diag.Diagnostics) map[string]any {
	config := make(map[string]any, len(r.kind.fields))
	for _, f := range r.kind.fields {
		var v types.String
		diags.Append(src.GetAttribute(ctx, path.Root(f.name), &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			config[f.name] = v.ValueString()
		}
	}
	return config
}

// fieldValue maps one config value onto its attribute. Absent or empty reads
// as null so a key never written is not drift.
func fieldValue(raw any) types.String {
	if s, _ := raw.(string); s != "" {
		return types.StringValue(s)
	}
	return types.StringNull()
}

// identityValues maps the store's identity onto its attributes, null when
// the store has none.
func identityValues(identity *secretStoreIdentity) (issuer, subject types.String) {
	if identity == nil {
		return types.StringNull(), types.StringNull()
	}
	return types.StringValue(identity.Issuer), types.StringValue(identity.Subject)
}

// persist writes a store back to state as the server holds it.
func (r *secretStoreResource) persist(ctx context.Context, state *tfsdk.State, store *secretStore, diags *diag.Diagnostics) {
	diags.Append(state.SetAttribute(ctx, path.Root("id"), store.ID)...)
	diags.Append(state.SetAttribute(ctx, path.Root("name"), store.Name)...)
	diags.Append(state.SetAttribute(ctx, path.Root("created_at"), store.CreatedAt)...)
	diags.Append(state.SetAttribute(ctx, path.Root("updated_at"), store.UpdatedAt)...)

	issuer, subject := identityValues(store.Identity)
	diags.Append(state.SetAttribute(ctx, path.Root("issuer"), issuer)...)
	diags.Append(state.SetAttribute(ctx, path.Root("subject"), subject)...)

	for _, f := range r.kind.fields {
		diags.Append(state.SetAttribute(ctx, path.Root(f.name), fieldValue(store.Config[f.name]))...)
	}
}

func (r *secretStoreResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var name types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("name"), &name)...)
	config := r.configFrom(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	store, err := r.client.CreateSecretStore(ctx, createSecretStoreRequest{
		Name:      name.ValueString(),
		StoreType: r.kind.storeType,
		Config:    config,
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Create Secret Store", err.Error())
		return
	}

	tflog.Info(ctx, "Secret store created", map[string]any{
		"id":         store.ID,
		"name":       store.Name,
		"store_type": store.StoreType,
	})

	r.persist(ctx, &resp.State, store, &resp.Diagnostics)
}

func (r *secretStoreResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	store, err := r.client.GetSecretStore(ctx, id.ValueString())
	if err != nil {
		if errors.Is(err, errNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to Read Secret Store", err.Error())
		return
	}

	// Each type is its own resource, so a store of another type under this
	// id is an import aimed at the wrong resource, not drift to reconcile.
	if store.StoreType != r.kind.storeType {
		resp.Diagnostics.AddError("Secret Store Type Mismatch",
			fmt.Sprintf("Secret store %s is a %s store; import it into marmot_secret_store_%s instead.",
				store.ID, store.StoreType, store.StoreType))
		return
	}

	r.persist(ctx, &resp.State, store, &resp.Diagnostics)
}

func (r *secretStoreResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	config := r.configFrom(ctx, req.Plan, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	// The whole config goes every time. The server replaces what it stores
	// with what is sent, so a key removed from the configuration is removed
	// from the store rather than lingering.
	store, err := r.client.UpdateSecretStore(ctx, id.ValueString(), updateSecretStoreRequest{Config: config})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Update Secret Store", err.Error())
		return
	}

	tflog.Info(ctx, "Secret store updated", map[string]any{
		"id":   store.ID,
		"name": store.Name,
	})

	r.persist(ctx, &resp.State, store, &resp.Diagnostics)
}

func (r *secretStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A store already gone is the outcome Delete wanted. A store with a
	// secret still referenced by a pipeline or a lease is refused, and stays.
	if err := r.client.DeleteSecretStore(ctx, id.ValueString()); err != nil && !errors.Is(err, errNotFound) {
		resp.Diagnostics.AddError("Unable to Delete Secret Store", err.Error())
		return
	}

	tflog.Info(ctx, "Secret store deleted", map[string]any{
		"id": id.ValueString(),
	})
}

func (r *secretStoreResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
