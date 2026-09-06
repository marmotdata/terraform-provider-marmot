// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// iamKind is how much of a resource's policy a Terraform resource owns.
//
// The three levels mirror the google_*_iam_* family, and for the same reason:
// one team wanting to manage the whole policy and another wanting to add a
// single member are different jobs, and conflating them makes one of them
// destructive.
type iamKind int

const (
	// iamKindPolicy owns the entire policy. Anything not in the configuration
	// is removed.
	iamKindPolicy iamKind = iota
	// iamKindBinding owns one role. Other roles are left alone.
	iamKindBinding
	// iamKindMember owns one (role, member) pair and nothing else.
	iamKindMember
)

// iamTarget describes one node of the Marmot resource hierarchy.
type iamTarget struct {
	// typePrefix is the Terraform type name, e.g. "asset" for
	// marmot_asset_iam_binding.
	typePrefix string
	// apiType is the path segment the API uses, in the API's own camelCase.
	apiType string
	// idPrefix is the first segment of a Terraform id and of an import id. It
	// follows the resource type name, not apiType, so an import id reads like
	// the resource it addresses.
	idPrefix string
	// idAttr is the Terraform attribute naming the resource; empty for the
	// organization, which is the root and has no id.
	idAttr string
	// label is used in documentation strings.
	label string
}

var iamTargets = []iamTarget{
	{typePrefix: "organization", apiType: "root", idPrefix: "root", idAttr: "", label: "the whole catalog"},
	{typePrefix: "asset", apiType: "asset", idPrefix: "asset", idAttr: "asset_id", label: "an asset"},
	{typePrefix: "data_product", apiType: "dataProduct", idPrefix: "data_product", idAttr: "data_product_id", label: "a data product and every asset it resolves"},
	{typePrefix: "glossary_term", apiType: "glossaryTerm", idPrefix: "glossary_term", idAttr: "glossary_term_id", label: "a glossary term and its descendants"},
}

// IAMResources returns every combination of hierarchy node and authority level.
func IAMResources() []func() resource.Resource {
	var out []func() resource.Resource
	for _, target := range iamTargets {
		for _, kind := range []iamKind{iamKindPolicy, iamKindBinding, iamKindMember} {
			out = append(out, func() resource.Resource {
				return &iamResource{target: target, kind: kind}
			})
		}
	}
	return out
}

type iamResource struct {
	target iamTarget
	kind   iamKind
	client *iamClient
}

var _ resource.Resource = &iamResource{}
var _ resource.ResourceWithImportState = &iamResource{}

func (r *iamResource) suffix() string {
	switch r.kind {
	case iamKindPolicy:
		return "_iam_policy"
	case iamKindBinding:
		return "_iam_binding"
	default:
		return "_iam_member"
	}
}

func (r *iamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_" + r.target.typePrefix + r.suffix()
}

func (r *iamResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Provider Data",
			fmt.Sprintf("Expected *marmot.Client, got %T", req.ProviderData))
		return
	}
	r.client = newIAMClient(client)
}

func (r *iamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	attrs := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			MarkdownDescription: "Terraform identifier for this grant.",
			Computed:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
		},
		"etag": schema.StringAttribute{
			MarkdownDescription: "Version of the policy as last read. Used to detect a concurrent change.",
			Computed:            true,
		},
	}

	if r.target.idAttr != "" {
		attrs[r.target.idAttr] = schema.StringAttribute{
			MarkdownDescription: "ID of the resource the grant applies to.",
			Required:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
		}
	}

	var description string
	switch r.kind {
	case iamKindPolicy:
		description = "Authoritative. Sets the complete access policy on " + r.target.label +
			", removing any binding not present in the configuration. Do not use alongside " +
			"`_iam_binding` or `_iam_member` for the same resource: they will fight."
		attrs["policy_data"] = schema.StringAttribute{
			MarkdownDescription: "Policy JSON, normally taken from the `marmot_iam_policy` data source.",
			Required:            true,
		}
	case iamKindBinding:
		description = "Authoritative for one role on " + r.target.label +
			". Other roles are left alone, but any member of this role not in the " +
			"configuration is removed."
		attrs["role"] = schema.StringAttribute{
			MarkdownDescription: "Marmot role name, for example `viewer`. A `roles/` prefix is accepted.",
			Required:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
		}
		attrs["members"] = schema.SetAttribute{
			MarkdownDescription: "Members holding the role: `user:{id}`, `group:{team id}`, " +
				"`serviceAccount:{id}`, or `allAuthenticated`. At least one is required; " +
				"a role with no members is not a grant, so remove the resource instead.",
			Required:    true,
			ElementType: types.StringType,
			Validators: []validator.Set{
				// Marmot drops a binding that holds nobody, so an empty set
				// would apply and then read back as absent, forever.
				setvalidator.SizeAtLeast(1),
			},
		}
	default:
		description = "Non-authoritative. Grants one member one role on " + r.target.label +
			", leaving every other member and role untouched."
		attrs["role"] = schema.StringAttribute{
			MarkdownDescription: "Marmot role name, for example `viewer`. A `roles/` prefix is accepted.",
			Required:            true,
			PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
		}
		attrs["member"] = schema.StringAttribute{
			MarkdownDescription: "Member to grant the role to: `user:{id}`, `group:{team id}`, " +
				"`serviceAccount:{id}`, or `allAuthenticated`.",
			Required:      true,
			PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()},
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: iamCloudOnly + description +
			"\n\nGrants are additive and there are no denies, so a " +
			"member also holding the permission over the whole catalog keeps it here. Restricting a " +
			"principal means giving it a role that does not carry the permission at the organization " +
			"level, then granting it on specific resources.",
		Attributes: attrs,
	}
}

// iamCloudOnly heads every access-grant page. Configuring the provider makes no
// request, and a resource being created is not read beforehand, so without this
// the first clue is a failed apply.
const iamCloudOnly = "~> **Requires Marmot Cloud or Marmot Enterprise.** Open-source Marmot " +
	"serves no access-policy API, so these resources fail on apply rather than at plan. " +
	"[Marmot Cloud](https://cloud.marmotdata.io) includes them on every plan, Free included.\n\n"

// The framework requires a model struct matching the schema exactly, and the
// schema differs per kind, so CRUD reads and writes attributes individually.

func (r *iamResource) targetID(ctx context.Context, src attrGetter, diags *diag.Diagnostics) (string, bool) {
	if r.target.idAttr == "" {
		return "", true
	}
	var id types.String
	diags.Append(src.GetAttribute(ctx, path.Root(r.target.idAttr), &id)...)
	if diags.HasError() {
		return "", false
	}
	return id.ValueString(), true
}

// attrGetter is satisfied by tfsdk.Plan, tfsdk.State and tfsdk.Config.
type attrGetter interface {
	GetAttribute(ctx context.Context, p path.Path, target any) diag.Diagnostics
}

// stateID has to distinguish two resources managing different roles on one
// catalog resource, so the role — and for a member resource the member — are
// part of it.
func (r *iamResource) stateID(resourceID, role, member string) string {
	parts := []string{r.target.idPrefix}
	if resourceID != "" {
		parts = append(parts, resourceID)
	}
	if role != "" {
		parts = append(parts, "roles/"+role)
	}
	if member != "" {
		parts = append(parts, member)
	}
	return strings.Join(parts, "/")
}

func normaliseRole(role string) string {
	return strings.TrimPrefix(strings.TrimSpace(role), "roles/")
}

func (r *iamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

func (r *iamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	r.apply(ctx, req.Plan, &resp.State, &resp.Diagnostics)
}

// apply writes the configured grant, then records the resulting etag.
func (r *iamResource) apply(ctx context.Context, plan attrGetter, state *tfsdk.State, diags *diag.Diagnostics) {
	resourceID, ok := r.targetID(ctx, plan, diags)
	if !ok {
		return
	}

	var role, member types.String
	var members types.Set
	var policyData types.String

	switch r.kind {
	case iamKindPolicy:
		diags.Append(plan.GetAttribute(ctx, path.Root("policy_data"), &policyData)...)
	case iamKindBinding:
		diags.Append(plan.GetAttribute(ctx, path.Root("role"), &role)...)
		diags.Append(plan.GetAttribute(ctx, path.Root("members"), &members)...)
	default:
		diags.Append(plan.GetAttribute(ctx, path.Root("role"), &role)...)
		diags.Append(plan.GetAttribute(ctx, path.Root("member"), &member)...)
	}
	if diags.HasError() {
		return
	}

	roleName := normaliseRole(role.ValueString())

	var memberList []string
	if r.kind == iamKindBinding {
		diags.Append(members.ElementsAs(ctx, &memberList, false)...)
		if diags.HasError() {
			return
		}
	}

	// Parsed before anything is written, so a malformed document is an error
	// rather than a policy silently emptied by a failed decode.
	var desired iamPolicy
	if r.kind == iamKindPolicy {
		if err := json.Unmarshal([]byte(policyData.ValueString()), &desired); err != nil {
			diags.AddError("Invalid policy_data", "Could not parse policy JSON: "+err.Error())
			return
		}
	}

	err := r.client.modifyPolicy(ctx, r.target.apiType, resourceID, func(p *iamPolicy) {
		switch r.kind {
		case iamKindPolicy:
			p.Bindings = desired.Bindings
		case iamKindBinding:
			p.setRole(roleName, memberList)
		default:
			p.addMember(roleName, member.ValueString())
		}
	})
	if err != nil {
		diags.AddError("Unable to Write Access Policy", err.Error())
		return
	}

	updated, err := r.client.GetPolicy(ctx, r.target.apiType, resourceID)
	if err != nil {
		diags.AddError("Unable to Read Access Policy", err.Error())
		return
	}

	tflog.Info(ctx, "Wrote Marmot access policy", map[string]any{
		"resource_type": r.target.apiType,
		"resource_id":   resourceID,
		"bindings":      len(updated.Bindings),
	})

	r.persist(ctx, state, diags, resourceID, roleName, member.ValueString(), role, members, policyData, updated)
}

// persist writes the applied grant back to state.
//
// role and members go back exactly as configured, never normalised: Terraform
// requires the state of a non-computed attribute to equal the plan, so
// rewriting "roles/viewer" to "viewer" fails the apply with "provider produced
// inconsistent result". Normalisation belongs on the wire, not in state.
func (r *iamResource) persist(
	ctx context.Context,
	state *tfsdk.State,
	diags *diag.Diagnostics,
	resourceID, roleName, member string,
	role types.String,
	members types.Set,
	policyData types.String,
	policy *iamPolicy,
) {
	if r.target.idAttr != "" {
		diags.Append(state.SetAttribute(ctx, path.Root(r.target.idAttr), resourceID)...)
	}
	diags.Append(state.SetAttribute(ctx, path.Root("id"), r.stateID(resourceID, roleName, member))...)
	diags.Append(state.SetAttribute(ctx, path.Root("etag"), policy.Etag)...)

	switch r.kind {
	case iamKindPolicy:
		diags.Append(state.SetAttribute(ctx, path.Root("policy_data"), policyData)...)
	case iamKindBinding:
		diags.Append(state.SetAttribute(ctx, path.Root("role"), role)...)
		diags.Append(state.SetAttribute(ctx, path.Root("members"), members)...)
	default:
		diags.Append(state.SetAttribute(ctx, path.Root("role"), role)...)
		diags.Append(state.SetAttribute(ctx, path.Root("member"), member)...)
	}
}

func (r *iamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	resourceID, ok := r.targetID(ctx, req.State, &resp.Diagnostics)
	if !ok {
		return
	}

	var role, member, policyData types.String
	if r.kind != iamKindPolicy {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("role"), &role)...)
	}
	if r.kind == iamKindMember {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("member"), &member)...)
	}
	if r.kind == iamKindPolicy {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("policy_data"), &policyData)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	policy, err := r.client.GetPolicy(ctx, r.target.apiType, resourceID)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Read Access Policy", err.Error())
		return
	}

	roleName := normaliseRole(role.ValueString())

	switch r.kind {
	case iamKindPolicy:
		remote := iamPolicy{Bindings: policy.Bindings}
		// Keep the configured document while it still says what the server
		// does, so formatting is not mistaken for a change in access.
		var stored iamPolicy
		if err := json.Unmarshal([]byte(policyData.ValueString()), &stored); err == nil && sameBindings(stored, remote) {
			break
		}
		canonicalisePolicy(&remote)
		encoded, err := json.Marshal(remote)
		if err != nil {
			resp.Diagnostics.AddError("Unable to Encode Policy", err.Error())
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("policy_data"), string(encoded))...)
	case iamKindBinding:
		current := policy.bindingFor(roleName)
		if len(current) == 0 {
			// The role holds nobody, so this resource no longer describes
			// anything that exists.
			resp.State.RemoveResource(ctx)
			return
		}
		set, d := types.SetValueFrom(ctx, types.StringType, normaliseMembers(current))
		resp.Diagnostics.Append(d...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("members"), set)...)
	default:
		if !slices.Contains(policy.bindingFor(roleName), member.ValueString()) {
			resp.State.RemoveResource(ctx)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("etag"), policy.Etag)...)
}

func (r *iamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resourceID, ok := r.targetID(ctx, req.State, &resp.Diagnostics)
	if !ok {
		return
	}

	var role, member types.String
	if r.kind != iamKindPolicy {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("role"), &role)...)
	}
	if r.kind == iamKindMember {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("member"), &member)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}
	roleName := normaliseRole(role.ValueString())

	err := r.client.modifyPolicy(ctx, r.target.apiType, resourceID, func(p *iamPolicy) {
		switch r.kind {
		case iamKindPolicy:
			// Destroying an authoritative policy clears it, which is what
			// "authoritative" means: this resource owned the whole thing.
			p.Bindings = nil
		case iamKindBinding:
			p.setRole(roleName, nil)
		default:
			p.removeMember(roleName, member.ValueString())
		}
	})
	if err != nil {
		// Deleting the target takes its policy with it, and the server then
		// refuses every write addressed to it — including this revoke. Ask
		// what the policy says now rather than matching on the error text.
		if current, readErr := r.client.GetPolicy(ctx, r.target.apiType, resourceID); readErr == nil && r.revoked(current, roleName, member.ValueString()) {
			tflog.Info(ctx, "Marmot access grant was already gone", map[string]any{
				"resource_type": r.target.apiType,
				"resource_id":   resourceID,
				"role":          roleName,
			})
			return
		}
		resp.Diagnostics.AddError("Unable to Revoke Access", err.Error())
	}
}

// revoked reports whether the policy no longer carries what this resource owned.
func (r *iamResource) revoked(policy *iamPolicy, roleName, member string) bool {
	switch r.kind {
	case iamKindPolicy:
		return len(policy.Bindings) == 0
	case iamKindBinding:
		return len(policy.bindingFor(roleName)) == 0
	default:
		return !slices.Contains(policy.bindingFor(roleName), member)
	}
}

// ImportState accepts the same id this resource writes, so an import round
// trips: "asset/{id}/roles/{role}" for a binding, plus "/{member}" for a member.
func (r *iamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) < 1 || parts[0] != r.target.idPrefix {
		resp.Diagnostics.AddError("Unexpected Import ID",
			fmt.Sprintf("Expected an id beginning with %q, got %q", r.target.idPrefix, req.ID))
		return
	}
	parts = parts[1:]

	if r.target.idAttr != "" {
		if len(parts) == 0 {
			resp.Diagnostics.AddError("Unexpected Import ID", "Missing resource id in "+req.ID)
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root(r.target.idAttr), parts[0])...)
		parts = parts[1:]
	}

	if r.kind != iamKindPolicy {
		if len(parts) < 2 || parts[0] != "roles" {
			resp.Diagnostics.AddError("Unexpected Import ID", "Missing roles/{role} in "+req.ID)
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role"), parts[1])...)
		parts = parts[2:]
	}

	if r.kind == iamKindMember {
		if len(parts) == 0 {
			resp.Diagnostics.AddError("Unexpected Import ID", "Missing member in "+req.ID)
			return
		}
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("member"), strings.Join(parts, "/"))...)
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
