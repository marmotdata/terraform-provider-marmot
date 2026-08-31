// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var _ datasource.DataSource = &IAMPolicyDataSource{}

func NewIAMPolicyDataSource() datasource.DataSource { return &IAMPolicyDataSource{} }

// IAMPolicyDataSource renders binding blocks into the JSON document the
// authoritative `_iam_policy` resources take. It makes no API calls: it exists
// so a policy can be written as HCL blocks rather than as an inline JSON string.
type IAMPolicyDataSource struct{}

type iamPolicyDataSourceModel struct {
	Binding    []iamPolicyBindingModel `tfsdk:"binding"`
	PolicyData types.String            `tfsdk:"policy_data"`
}

type iamPolicyBindingModel struct {
	Role    types.String `tfsdk:"role"`
	Members types.Set    `tfsdk:"members"`
}

func (d *IAMPolicyDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_iam_policy"
}

func (d *IAMPolicyDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Builds a policy document for the authoritative `*_iam_policy` resources. " +
			"Purely local: it renders its blocks to JSON and contacts no server.",
		Attributes: map[string]schema.Attribute{
			"policy_data": schema.StringAttribute{
				MarkdownDescription: "The rendered policy, for a resource's `policy_data`.",
				Computed:            true,
			},
		},
		Blocks: map[string]schema.Block{
			"binding": schema.ListNestedBlock{
				MarkdownDescription: "One role and the members that hold it.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"role": schema.StringAttribute{
							MarkdownDescription: "Marmot role name, for example `viewer`.",
							Required:            true,
						},
						"members": schema.SetAttribute{
							MarkdownDescription: "Members holding the role: `user:{id}`, " +
								"`group:{team id}`, `serviceAccount:{id}`, or `allAuthenticated`.",
							Required:    true,
							ElementType: types.StringType,
						},
					},
				},
			},
		},
	}
}

func (d *IAMPolicyDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config iamPolicyDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Bindings are merged by role so two blocks naming the same role add up
	// rather than one of them being silently dropped.
	byRole := map[string][]string{}
	var order []string
	for _, b := range config.Binding {
		role := normaliseRole(b.Role.ValueString())
		var members []string
		resp.Diagnostics.Append(b.Members.ElementsAs(ctx, &members, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if _, seen := byRole[role]; !seen {
			order = append(order, role)
		}
		byRole[role] = append(byRole[role], members...)
	}

	policy := iamPolicy{Bindings: make([]iamBinding, 0, len(order))}
	for _, role := range order {
		policy.Bindings = append(policy.Bindings, iamBinding{
			Role:    role,
			Members: normaliseMembers(byRole[role]),
		})
	}
	canonicalisePolicy(&policy)

	encoded, err := json.Marshal(policy)
	if err != nil {
		resp.Diagnostics.AddError("Unable to Render Policy", err.Error())
		return
	}

	config.PolicyData = types.StringValue(string(encoded))
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
