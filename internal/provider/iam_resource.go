// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// iamKind is how much of a resource's policy a Terraform resource owns. The
// three levels mirror the google_*_iam_* family: owning a whole policy and
// adding a single member are different jobs, and merging them makes one of
// them destructive.
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
	typePrefix string // Terraform type name, e.g. "asset" for marmot_asset_iam_binding
	apiType    string // path segment the API uses
	idAttr     string // attribute naming the resource; empty for the organization, which has no id
	label      string // used in the documentation strings
}

var iamTargets = []iamTarget{
	{typePrefix: "organization", apiType: "root", idAttr: "", label: "the whole catalog"},
	{typePrefix: "asset", apiType: "asset", idAttr: "asset_id", label: "an asset"},
	{typePrefix: "data_product", apiType: "data_product", idAttr: "data_product_id", label: "a data product and every asset it resolves"},
	{typePrefix: "glossary_term", apiType: "glossary_term", idAttr: "glossary_term_id", label: "a glossary term and its descendants"},
}

// IAMResources returns every combination of hierarchy node and authority level.
func IAMResources() []func() resource.Resource {
	var out []func() resource.Resource
	for _, target := range iamTargets {
		for _, kind := range []iamKind{iamKindPolicy, iamKindBinding, iamKindMember} {
			t, k := target, kind
			out = append(out, func() resource.Resource { return &iamResource{target: t, kind: k} })
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
			", removing any binding not in the configuration. Do not combine it with " +
			"`_iam_binding` or `_iam_member` on the same resource: they overwrite each other."
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
				"`serviceAccount:{id}`, or `allAuthenticated`.",
			Required:    true,
			ElementType: types.StringType,
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
		MarkdownDescription: description + "\n\nGrants are additive and there are no denies. A member " +
			"that already holds the permission over the whole catalog keeps it here. To restrict a " +
			"principal, give it an organization-level role without the permission and grant that " +
			"permission on specific resources instead.",
		Attributes: attrs,
	}
}

// A model struct has to match the schema exactly and the schema differs per
// kind, so CRUD reads and writes attributes one at a time instead.

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

// stateID is a stable Terraform identifier. Two resources can manage different
// roles on the same catalog resource, so the role — and the member, for a
// member resource — are part of it.
func (r *iamResource) stateID(resourceID, role, member string) string {
	parts := []string{r.target.apiType}
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

	var parsed iamPolicy
	if r.kind == iamKindPolicy {
		if err := json.Unmarshal([]byte(policyData.ValueString()), &parsed); err != nil {
			diags.AddError("Invalid policy_data", "Could not parse policy JSON: "+err.Error())
			return
		}
	}

	err := r.client.modifyPolicy(ctx, r.target.apiType, resourceID, func(p *iamPolicy) {
		switch r.kind {
		case iamKindPolicy:
			p.Bindings = parsed.Bindings
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

	r.persist(ctx, state, diags, resourceID, roleName, member.ValueString(), memberList, policyData, updated)
}

func (r *iamResource) persist(
	ctx context.Context,
	state *tfsdk.State,
	diags *diag.Diagnostics,
	resourceID, roleName, member string,
	memberList []string,
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
		diags.Append(state.SetAttribute(ctx, path.Root("role"), roleName)...)
		set, d := types.SetValueFrom(ctx, types.StringType, normaliseMembers(memberList))
		diags.Append(d...)
		diags.Append(state.SetAttribute(ctx, path.Root("members"), set)...)
	default:
		diags.Append(state.SetAttribute(ctx, path.Root("role"), roleName)...)
		diags.Append(state.SetAttribute(ctx, path.Root("member"), member)...)
	}
}

func (r *iamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
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

	policy, err := r.client.GetPolicy(ctx, r.target.apiType, resourceID)
	if err != nil {
		// A resource deleted outside Terraform takes its policy with it, so
		// treat a missing resource as drift rather than an error.
		if strings.Contains(err.Error(), "Not Found") {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to Read Access Policy", err.Error())
		return
	}

	roleName := normaliseRole(role.ValueString())

	switch r.kind {
	case iamKindPolicy:
		remote := iamPolicy{Bindings: policy.Bindings}
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
			// Nobody holds the role, so there is nothing left to manage.
			resp.State.RemoveResource(ctx)
			return
		}
		set, d := types.SetValueFrom(ctx, types.StringType, normaliseMembers(current))
		resp.Diagnostics.Append(d...)
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("members"), set)...)
	default:
		found := false
		for _, m := range policy.bindingFor(roleName) {
			if m == member.ValueString() {
				found = true
				break
			}
		}
		if !found {
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
			// This resource owned the whole policy, so destroying it clears it.
			p.Bindings = nil
		case iamKindBinding:
			p.setRole(roleName, nil)
		default:
			p.removeMember(roleName, member.ValueString())
		}
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to Revoke Access", err.Error())
	}
}

// ImportState accepts the same id this resource writes, so an import round
// trips: "asset/{id}/roles/{role}" for a binding, plus "/{member}" for a member.
func (r *iamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	parts := strings.Split(req.ID, "/")
	if len(parts) < 1 || parts[0] != r.target.apiType {
		resp.Diagnostics.AddError("Unexpected Import ID",
			fmt.Sprintf("Expected an id beginning with %q, got %q", r.target.apiType, req.ID))
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
