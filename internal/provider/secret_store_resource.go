// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
	// sensitive values come back masked, so the provider never learns them
	// from the server and keeps what it wrote.
	sensitive bool
	// serverDefault is a constant the server applies when the key is unset,
	// mirrored as the attribute's default so the plan knows it.
	serverDefault string
	oneOf         []string
	// derived marks a value the server fills in when the key is unset, from
	// the method and the sibling keys in derivedFrom. The plan keeps the
	// stored value until one of those changes.
	derived     bool
	derivedFrom []string
}

// attribute builds the schema attribute; parent is the path of the object
// the field sits in, so a derived field can name the siblings it follows.
func (f secretStoreField) attribute(parent path.Path) schema.Attribute {
	a := schema.StringAttribute{
		MarkdownDescription: f.description,
		Required:            f.required,
		Optional:            !f.required,
		Sensitive:           f.sensitive,
	}
	if f.serverDefault != "" {
		a.MarkdownDescription += " Defaults to `" + f.serverDefault + "`."
		a.Computed = true
		a.Default = stringdefault.StaticString(f.serverDefault)
	}
	if f.derived {
		a.Computed = true
		inputs := keepStateUnless{parent.AtName("method")}
		for _, name := range f.derivedFrom {
			inputs = append(inputs, parent.AtName(name))
		}
		a.PlanModifiers = []planmodifier.String{inputs}
	}
	if len(f.oneOf) > 0 {
		a.Validators = []validator.String{stringvalidator.OneOf(f.oneOf...)}
	}
	return a
}

// keepStateUnless plans the stored value of a computed attribute the server
// fills in, as long as none of the attributes it is derived from change. A
// changed or unknown input leaves it unknown for the server to set again.
type keepStateUnless []path.Path

var _ planmodifier.String = keepStateUnless(nil)

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
	// fields are the top-level config keys; auth the keys under "auth".
	fields []secretStoreField
	auth   []secretStoreField
}

// defaultMethod is the auth method the server applies without an auth block.
func (k secretStoreKind) defaultMethod() string {
	for _, f := range k.auth {
		if f.name == "method" {
			return f.serverDefault
		}
	}
	return ""
}

// authMethod is the `method` attribute every store carries, with the server's
// default for the type.
func authMethod(serverDefault string, methods ...string) secretStoreField {
	return secretStoreField{
		name: "method",
		description: "How the store authenticates. `default` uses the server's own credentials; " +
			"`federated` presents a Marmot-issued OIDC token for the subject `store:{name}`, " +
			"which the backend exchanges for a credential granted only what this store may reach.",
		serverDefault: serverDefault,
		oneOf:         methods,
	}
}

var secretStoreKinds = []secretStoreKind{
	{
		storeType: "google",
		label:     "Google Secret Manager",
		description: "A Google Secret Manager store. A ref names a secret and a version: " +
			"`{\"secret\": \"db-password\", \"version\": \"latest\"}`, with an optional `project` " +
			"overriding the store's.",
		fields: []secretStoreField{
			{name: "project", description: "Default project for refs that do not name one."},
		},
		auth: []secretStoreField{
			authMethod("default", "default", "federated"),
			{
				name: "workload_identity_provider",
				description: "Workload Identity Federation provider the Marmot token is exchanged at, " +
					"`projects/{number}/locations/global/workloadIdentityPools/{pool}/providers/{provider}` " +
					"(the `name` of a `google_iam_workload_identity_pool_provider`). Required for the " +
					"federated method.",
			},
			{
				name: "audience",
				description: "Audience the Marmot token carries at the exchange. Derived from " +
					"`workload_identity_provider` by the server (`//iam.googleapis.com/{provider}`) " +
					"when unset. Federated method only.",
				derived:     true,
				derivedFrom: []string{"workload_identity_provider"},
			},
			{
				name: "service_account",
				description: "Service account email to impersonate after federation. Empty " +
					"accesses Secret Manager directly as the federated principal.",
			},
		},
	},
	{
		storeType: "aws",
		label:     "AWS Secrets Manager",
		description: "An AWS Secrets Manager store. A ref names a secret by name or ARN: " +
			"`{\"secret_id\": \"prod/db-password\", \"version_stage\": \"AWSCURRENT\"}`.",
		fields: []secretStoreField{
			{name: "region", description: "Region to call Secrets Manager in. Defaults to the region of the server's credentials."},
		},
		auth: []secretStoreField{
			authMethod("default", "default", "federated"),
			{
				name: "role_arn",
				description: "IAM role to assume with the Marmot token. Its trust policy must name " +
					"the Marmot issuer as an OIDC provider. Required for the federated method.",
			},
			{
				name: "audience",
				description: "Audience the Marmot token carries: the OIDC provider's client ID, " +
					"which the role's trust policy conditions `aud` on. The server sets " +
					"`sts.amazonaws.com` when unset. Federated method only.",
				derived: true,
			},
			{
				name: "session_name",
				description: "Session name of the assumed role, visible in CloudTrail. The server " +
					"sets `marmot` when unset.",
				derived: true,
			},
		},
	},
	{
		storeType: "azure",
		label:     "Azure Key Vault",
		description: "An Azure Key Vault store. A ref names a secret and optionally a version: " +
			"`{\"name\": \"db-password\"}`.",
		fields: []secretStoreField{
			{name: "vault_url", description: "Key Vault URL, for example `https://my-vault.vault.azure.net`.", required: true},
		},
		auth: []secretStoreField{
			authMethod("default", "default", "federated"),
			{
				name: "tenant_id",
				description: "Entra tenant of the app registration the Marmot token is exchanged " +
					"for. Required for the federated method.",
			},
			{
				name: "client_id",
				description: "App registration (client) ID carrying a federated credential that " +
					"trusts the Marmot issuer. Required for the federated method.",
			},
			{
				name: "audience",
				description: "Audience the Marmot token carries, matching the federated credential's. " +
					"The server sets `api://AzureADTokenExchange` when unset. Federated method only.",
				derived: true,
			},
		},
	},
	{
		storeType: "vault",
		label:     "HashiCorp Vault",
		description: "A HashiCorp Vault KV v2 store. A ref names a path and a key: " +
			"`{\"mount\": \"secret\", \"path\": \"agents/analytics\", \"key\": \"api_key\"}`, " +
			"with an optional integer `version`.",
		fields: []secretStoreField{
			{name: "address", description: "Vault server URL.", required: true},
			{name: "namespace", description: "Vault Enterprise namespace, sent as `X-Vault-Namespace`."},
			{
				name: "ca_cert",
				description: "PEM-encoded CA certificate to verify the Vault server's TLS certificate " +
					"against. Omit to use the system roots.",
			},
		},
		auth: []secretStoreField{
			{
				name: "method",
				description: "How the store authenticates. `kubernetes` presents the pod's service " +
					"account token; `federated` presents a Marmot-issued OIDC token to the JWT auth " +
					"method; `token` uses a static token.",
				serverDefault: "kubernetes",
				oneOf:         []string{"kubernetes", "token", "federated"},
			},
			{name: "role", description: "Vault role to log in as with the kubernetes and federated methods."},
			{name: "token", description: "Vault token, for development only. Token method only.", sensitive: true},
			{
				name: "token_path",
				description: "Path of the service account JWT presented to the Kubernetes auth " +
					"method. Defaults to `/var/run/secrets/kubernetes.io/serviceaccount/token`.",
			},
			{name: "mount_path", description: "Mount path of the Kubernetes auth method. Defaults to `kubernetes`."},
			{name: "jwt_mount_path", description: "Mount path of the JWT auth method the Marmot token is presented to. Defaults to `jwt`."},
			{
				name: "audience",
				description: "Audience the Marmot token must carry: the JWT role's `bound_audiences`. " +
					"Unset, the token carries the issuer URL. Federated method only.",
			},
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

// identityInputs are the attributes a store's identity follows: the subject
// is the name, and the rest is only there under federated auth.
var identityInputs = keepStateUnless{path.Root("name"), path.Root("auth")}

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
	attrs := map[string]schema.Attribute{
		"name": schema.StringAttribute{
			MarkdownDescription: "Name of the store, unique per instance. Under federated auth it is " +
				"also the store's identity: the token subject is `store:{name}`. Changing it " +
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
			MarkdownDescription: "Subject of the tokens the store presents, `store:{name}`: the value " +
				"to grant on the cloud side, such as the subject of a `principal://` member on " +
				"Google Cloud or the `sub` condition of an AWS role trust policy. Null unless " +
				"`auth.method` is `federated`.",
			Computed:      true,
			PlanModifiers: []planmodifier.String{identityInputs},
		},
		"issuer": schema.StringAttribute{
			MarkdownDescription: "Issuer URL of the tokens the store presents: the OIDC provider to " +
				"register with the cloud identity provider, once per account. Null unless " +
				"`auth.method` is `federated`.",
			Computed:      true,
			PlanModifiers: []planmodifier.String{identityInputs},
		},
		"audience": schema.StringAttribute{
			MarkdownDescription: "Audience the store's tokens carry, which the cloud identity provider " +
				"must expect: `auth.audience`, or what the server derives when that is unset. Null " +
				"unless `auth.method` is `federated`.",
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
		attrs[f.name] = f.attribute(path.Empty())
	}

	authAttrs := make(map[string]schema.Attribute, len(r.kind.auth))
	for _, f := range r.kind.auth {
		authAttrs[f.name] = f.attribute(path.Root("auth"))
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: secretStoreCloudOnly + r.kind.description +
			"\n\nA store holds no secret values: pipelines reference them with a `secret` block on " +
			"`marmot_pipeline` and `marmot_service_account_lease` writes short-lived keys through " +
			"the store, both resolved at run time. Sensitive settings are encrypted at rest and " +
			"never read back, so state keeps what was written.\n\n" +
			"Under federated auth the store has an OIDC identity of its own: `issuer`, `subject` " +
			"and `audience` are what to trust and grant on the cloud side, so the store reaches " +
			"only the secrets bound to it.",
		Attributes: attrs,
		Blocks: map[string]schema.Block{
			"auth": schema.SingleNestedBlock{
				MarkdownDescription: "How the store authenticates to " + r.kind.label +
					". Omit to use the server's own credentials.",
				Attributes: authAttrs,
			},
		},
	}
}

func (r *secretStoreResource) authAttrTypes() map[string]attr.Type {
	out := make(map[string]attr.Type, len(r.kind.auth))
	for _, f := range r.kind.auth {
		out[f.name] = types.StringType
	}
	return out
}

// configFrom assembles the config the API takes from the planned attributes.
// Null and unknown attributes are left out so the server applies its own
// defaults and derivations; a derived value the plan kept from state goes
// back as is, which the server accepts since it is what it would derive.
func (r *secretStoreResource) configFrom(ctx context.Context, src attrGetter, diags *diag.Diagnostics) map[string]any {
	config := make(map[string]any, len(r.kind.fields)+1)
	for _, f := range r.kind.fields {
		var v types.String
		diags.Append(src.GetAttribute(ctx, path.Root(f.name), &v)...)
		if !v.IsNull() && !v.IsUnknown() {
			config[f.name] = v.ValueString()
		}
	}

	var auth types.Object
	diags.Append(src.GetAttribute(ctx, path.Root("auth"), &auth)...)
	if diags.HasError() {
		return nil
	}
	if auth.IsNull() || auth.IsUnknown() {
		return config
	}
	authConfig := make(map[string]any, len(r.kind.auth))
	for _, f := range r.kind.auth {
		if v := objectString(auth, f.name); !v.IsNull() && !v.IsUnknown() {
			authConfig[f.name] = v.ValueString()
		}
	}
	config["auth"] = authConfig
	return config
}

// objectString reads one string attribute of an object, null when the object
// is null or unknown.
func objectString(obj types.Object, name string) types.String {
	if obj.IsNull() || obj.IsUnknown() {
		return types.StringNull()
	}
	v, ok := obj.Attributes()[name].(types.String)
	if !ok {
		return types.StringNull()
	}
	return v
}

// fieldValue maps one config value onto its attribute. A sensitive value
// comes back masked, so the prior value is kept and the mask never reaches
// state. Absent or empty reads as null so a key never written is not drift.
func fieldValue(f secretStoreField, raw any, prior types.String) types.String {
	s, _ := raw.(string)
	if f.sensitive && s == sensitiveMask {
		return prior
	}
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// identityValues maps the store's identity onto its three attributes, all
// null when the store has none.
func identityValues(identity *secretStoreIdentity) (issuer, subject, audience types.String) {
	if identity == nil {
		return types.StringNull(), types.StringNull(), types.StringNull()
	}
	return types.StringValue(identity.Issuer), types.StringValue(identity.Subject), types.StringValue(identity.Audience)
}

// persist writes a store back to state. Sensitive values are sourced from
// prior: the plan on create and update, the state on read.
func (r *secretStoreResource) persist(ctx context.Context, state *tfsdk.State, prior attrGetter, store *secretStore, diags *diag.Diagnostics) {
	diags.Append(state.SetAttribute(ctx, path.Root("id"), store.ID)...)
	diags.Append(state.SetAttribute(ctx, path.Root("name"), store.Name)...)
	diags.Append(state.SetAttribute(ctx, path.Root("created_at"), store.CreatedAt)...)
	diags.Append(state.SetAttribute(ctx, path.Root("updated_at"), store.UpdatedAt)...)

	issuer, subject, audience := identityValues(store.Identity)
	diags.Append(state.SetAttribute(ctx, path.Root("issuer"), issuer)...)
	diags.Append(state.SetAttribute(ctx, path.Root("subject"), subject)...)
	diags.Append(state.SetAttribute(ctx, path.Root("audience"), audience)...)

	for _, f := range r.kind.fields {
		var kept types.String
		if f.sensitive {
			diags.Append(prior.GetAttribute(ctx, path.Root(f.name), &kept)...)
		}
		diags.Append(state.SetAttribute(ctx, path.Root(f.name), fieldValue(f, store.Config[f.name], kept))...)
	}

	attrTypes := r.authAttrTypes()
	var priorAuth types.Object
	diags.Append(prior.GetAttribute(ctx, path.Root("auth"), &priorAuth)...)
	raw, ok := store.Config["auth"].(map[string]any)
	// The server returns its own defaults for a store written without an
	// auth block. Putting them in state would plan the block's removal on
	// every run, so they stay out unless the method is not the default,
	// which only a store set up outside Terraform can have.
	if !ok || (priorAuth.IsNull() && raw["method"] == r.kind.defaultMethod()) {
		diags.Append(state.SetAttribute(ctx, path.Root("auth"), types.ObjectNull(attrTypes))...)
		return
	}
	values := make(map[string]attr.Value, len(r.kind.auth))
	for _, f := range r.kind.auth {
		values[f.name] = fieldValue(f, raw[f.name], objectString(priorAuth, f.name))
	}
	auth, d := types.ObjectValue(attrTypes, values)
	diags.Append(d...)
	diags.Append(state.SetAttribute(ctx, path.Root("auth"), auth)...)
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

	r.persist(ctx, &resp.State, req.Plan, store, &resp.Diagnostics)
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

	r.persist(ctx, &resp.State, req.State, store, &resp.Diagnostics)
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

	r.persist(ctx, &resp.State, req.Plan, store, &resp.Diagnostics)
}

func (r *secretStoreResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// A store already gone is the outcome Delete wanted. A store still
	// referenced by a pipeline or a lease is refused, and stays.
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
