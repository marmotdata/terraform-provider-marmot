// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64default"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &DataProductRuleResource{}
var _ resource.ResourceWithImportState = &DataProductRuleResource{}
var _ resource.ResourceWithValidateConfig = &DataProductRuleResource{}

func NewDataProductRuleResource() resource.Resource {
	return &DataProductRuleResource{}
}

// DataProductRuleResource defines the resource implementation.
type DataProductRuleResource struct {
	client *marmot.Client
}

// DataProductRuleResourceModel describes the data product rule resource data model.
type DataProductRuleResourceModel struct {
	DataProductID   types.String `tfsdk:"data_product_id"`
	Name            types.String `tfsdk:"name"`
	Description     types.String `tfsdk:"description"`
	Type            types.String `tfsdk:"type"`
	QueryExpression types.String `tfsdk:"query_expression"`
	MetadataField   types.String `tfsdk:"metadata_field"`
	PatternType     types.String `tfsdk:"pattern_type"`
	PatternValue    types.String `tfsdk:"pattern_value"`
	Priority        types.Int64  `tfsdk:"priority"`
	Enabled         types.Bool   `tfsdk:"enabled"`
	ID              types.String `tfsdk:"id"`
	CreatedAt       types.String `tfsdk:"created_at"`
	UpdatedAt       types.String `tfsdk:"updated_at"`
}

func (r *DataProductRuleResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_data_product_rule"
}

func (r *DataProductRuleResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Pulls assets into a data product dynamically, either with a search query " +
			"or by matching a metadata field against a pattern. A data product can have up to 10 rules.",

		Attributes: map[string]schema.Attribute{
			"data_product_id": schema.StringAttribute{
				MarkdownDescription: "ID of the data product the rule belongs to",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the rule",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the rule",
				Optional:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "`query` to match assets with a search query, or `metadata_match` " +
					"to match a metadata field against a pattern.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.OneOf("query", "metadata_match"),
				},
			},
			"query_expression": schema.StringAttribute{
				MarkdownDescription: "Search query for 'query' rules",
				Optional:            true,
			},
			"metadata_field": schema.StringAttribute{
				MarkdownDescription: "Metadata field to match for 'metadata_match' rules",
				Optional:            true,
			},
			"pattern_type": schema.StringAttribute{
				MarkdownDescription: "How the pattern is matched for 'metadata_match' rules: " +
					"'exact', 'wildcard', 'regex', or 'prefix'",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.OneOf("exact", "wildcard", "regex", "prefix"),
				},
			},
			"pattern_value": schema.StringAttribute{
				MarkdownDescription: "Pattern to match the metadata field against for 'metadata_match' rules",
				Optional:            true,
			},
			"priority": schema.Int64Attribute{
				MarkdownDescription: "Priority of the rule. Defaults to `0`.",
				Optional:            true,
				Computed:            true,
				Default:             int64default.StaticInt64(0),
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the rule is evaluated. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Rule ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
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
		},
	}
}

// ValidateConfig enforces the field combinations the Marmot API requires per
// rule type, surfacing the error at plan time rather than on apply. A 'query'
// rule needs a query_expression; a 'metadata_match' rule needs a
// metadata_field, pattern_type, and pattern_value.
func (r *DataProductRuleResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data DataProductRuleResourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Skip validation until the rule type is known.
	if data.Type.IsUnknown() || data.Type.IsNull() {
		return
	}

	switch data.Type.ValueString() {
	case "query":
		if isEmptyString(data.QueryExpression) {
			resp.Diagnostics.AddAttributeError(
				path.Root("query_expression"),
				"Missing query_expression",
				"query_expression is required when type is 'query'.",
			)
		}
	case "metadata_match":
		if isEmptyString(data.MetadataField) {
			resp.Diagnostics.AddAttributeError(
				path.Root("metadata_field"),
				"Missing metadata_field",
				"metadata_field is required when type is 'metadata_match'.",
			)
		}
		if isEmptyString(data.PatternType) {
			resp.Diagnostics.AddAttributeError(
				path.Root("pattern_type"),
				"Missing pattern_type",
				"pattern_type is required when type is 'metadata_match'.",
			)
		}
		if isEmptyString(data.PatternValue) {
			resp.Diagnostics.AddAttributeError(
				path.Root("pattern_value"),
				"Missing pattern_value",
				"pattern_value is required when type is 'metadata_match'.",
			)
		}
	}
}

// isEmptyString reports whether s is null, unknown, or the empty string.
func isEmptyString(s types.String) bool {
	return s.IsNull() || s.IsUnknown() || s.ValueString() == ""
}

func (r *DataProductRuleResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *marmot.Client, got: %T", req.ProviderData),
		)
		return
	}

	r.client = client
}

func (r *DataProductRuleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data DataProductRuleResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rule, err := r.client.DataProducts.CreateRule(ctx, data.DataProductID.ValueString(), r.toRuleInput(data))
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create data product rule: %s", err))
		return
	}

	if rule.ID == "" {
		resp.Diagnostics.AddError("API Error", "Data product rule created but no ID returned")
		return
	}

	applyDataProductRuleComputedFields(&data, rule)

	tflog.Info(ctx, "Data product rule created", map[string]any{
		"id":              data.ID.ValueString(),
		"data_product_id": data.DataProductID.ValueString(),
		"name":            data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataProductRuleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data DataProductRuleResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rules, err := r.client.DataProducts.Rules(ctx, data.DataProductID.ValueString())
	if err != nil {
		if marmot.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read data product rules: %s", err))
		return
	}

	var rule *marmot.DataProductRule
	for _, candidate := range rules {
		if candidate.ID == data.ID.ValueString() {
			rule = candidate
			break
		}
	}
	if rule == nil {
		resp.State.RemoveResource(ctx)
		return
	}

	r.updateModelFromResponse(&data, rule)

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataProductRuleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data DataProductRuleResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state DataProductRuleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rule, err := r.client.DataProducts.UpdateRule(ctx, state.DataProductID.ValueString(), state.ID.ValueString(), r.toRuleInput(data))
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update data product rule: %s", err))
		return
	}

	applyDataProductRuleComputedFields(&data, rule)

	tflog.Info(ctx, "Data product rule updated", map[string]any{
		"id":              data.ID.ValueString(),
		"data_product_id": data.DataProductID.ValueString(),
		"name":            data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *DataProductRuleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data DataProductRuleResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.DataProducts.DeleteRule(ctx, data.DataProductID.ValueString(), data.ID.ValueString()); err != nil {
		if marmot.IsNotFound(err) {
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete data product rule: %s", err))
		return
	}

	tflog.Info(ctx, "Data product rule deleted", map[string]any{
		"id":              data.ID.ValueString(),
		"data_product_id": data.DataProductID.ValueString(),
	})
}

// ImportState imports a rule with the composite ID "<data_product_id>/<rule_id>".
func (r *DataProductRuleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	productID, ruleID, ok := strings.Cut(req.ID, "/")
	if !ok || productID == "" || ruleID == "" {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			fmt.Sprintf("Expected import ID in the format 'data_product_id/rule_id', got: %s", req.ID),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("data_product_id"), productID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), ruleID)...)
}

func (r *DataProductRuleResource) toRuleInput(data DataProductRuleResourceModel) marmot.ProductRuleInput {
	return marmot.ProductRuleInput{
		Name:            data.Name.ValueString(),
		Description:     data.Description.ValueString(),
		RuleType:        data.Type.ValueString(),
		QueryExpression: data.QueryExpression.ValueString(),
		MetadataField:   data.MetadataField.ValueString(),
		PatternType:     data.PatternType.ValueString(),
		PatternValue:    data.PatternValue.ValueString(),
		Priority:        data.Priority.ValueInt64(),
		IsEnabled:       data.Enabled.ValueBool(),
	}
}

// applyDataProductRuleComputedFields copies the server-generated (read-only)
// attributes from an API response onto the model, leaving every configured
// attribute untouched.
func applyDataProductRuleComputedFields(model *DataProductRuleResourceModel, rule *marmot.DataProductRule) {
	model.ID = types.StringValue(rule.ID)
	model.CreatedAt = types.StringValue(rule.CreatedAt)
	model.UpdatedAt = types.StringValue(rule.UpdatedAt)
}

func (r *DataProductRuleResource) updateModelFromResponse(model *DataProductRuleResourceModel, rule *marmot.DataProductRule) {
	model.ID = types.StringValue(rule.ID)
	model.Name = types.StringValue(rule.Name)
	model.Type = types.StringValue(string(rule.RuleType))
	model.Priority = types.Int64Value(rule.Priority)
	model.Enabled = types.BoolValue(rule.IsEnabled)
	model.CreatedAt = types.StringValue(rule.CreatedAt)
	model.UpdatedAt = types.StringValue(rule.UpdatedAt)

	if rule.Description != "" {
		model.Description = types.StringValue(rule.Description)
	} else {
		model.Description = types.StringNull()
	}

	if rule.QueryExpression != "" {
		model.QueryExpression = types.StringValue(rule.QueryExpression)
	} else {
		model.QueryExpression = types.StringNull()
	}

	if rule.MetadataField != "" {
		model.MetadataField = types.StringValue(rule.MetadataField)
	} else {
		model.MetadataField = types.StringNull()
	}

	if rule.PatternType != "" {
		model.PatternType = types.StringValue(rule.PatternType)
	} else {
		model.PatternType = types.StringNull()
	}

	if rule.PatternValue != "" {
		model.PatternValue = types.StringValue(rule.PatternValue)
	} else {
		model.PatternValue = types.StringNull()
	}
}
