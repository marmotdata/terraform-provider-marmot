// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &PipelineResource{}
var _ resource.ResourceWithImportState = &PipelineResource{}

func NewPipelineResource() resource.Resource {
	return &PipelineResource{}
}

// PipelineResource defines the resource implementation.
type PipelineResource struct {
	client *marmot.Client
}

// PipelineResourceModel describes the pipeline resource data model.
type PipelineResourceModel struct {
	Name           types.String         `tfsdk:"name"`
	PluginID       types.String         `tfsdk:"plugin_id"`
	Config         jsontypes.Normalized `tfsdk:"config"`
	CronExpression types.String         `tfsdk:"cron_expression"`
	Enabled        types.Bool           `tfsdk:"enabled"`
	ID             types.String         `tfsdk:"id"`
	ManagedBy      types.String         `tfsdk:"managed_by"`
	LastRunStatus  types.String         `tfsdk:"last_run_status"`
	LastRunAt      types.String         `tfsdk:"last_run_at"`
	NextRunAt      types.String         `tfsdk:"next_run_at"`
	CreatedAt      types.String         `tfsdk:"created_at"`
	UpdatedAt      types.String         `tfsdk:"updated_at"`
}

func (r *PipelineResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
}

func (r *PipelineResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A pipeline: a plugin pointed at a source that discovers and catalogs " +
			"assets on a recurring schedule. Rather than declaring each asset by hand, point a plugin " +
			"at a source and Marmot keeps the catalog in sync from what it finds there.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the pipeline",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.LengthBetween(1, 255),
				},
			},
			"plugin_id": schema.StringAttribute{
				MarkdownDescription: "ID of the plugin that runs the ingestion, for example `postgresql`, " +
					"`bigquery` or `kafka`.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"config": schema.StringAttribute{
				MarkdownDescription: "Plugin configuration as a JSON object. The accepted keys depend on " +
					"the plugin; the server validates this against the plugin and rejects an invalid config. " +
					"Use `jsonencode()` to build it from HCL.",
				Required:   true,
				CustomType: jsontypes.NormalizedType{},
			},
			"cron_expression": schema.StringAttribute{
				MarkdownDescription: "Cron expression setting how often the pipeline runs, for example " +
					"`0 * * * *` for hourly.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the pipeline runs on its cron. Defaults to `true`. Set to " +
					"`false` to keep the pipeline but pause automatic runs.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Pipeline ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"managed_by": schema.StringAttribute{
				MarkdownDescription: "External controller that runs this pipeline, such as the Marmot " +
					"Kubernetes operator. Empty for Terraform-managed pipelines, which the server runs on their cron.",
				Computed: true,
			},
			"last_run_status": schema.StringAttribute{
				MarkdownDescription: "Status of the most recent run",
				Computed:            true,
			},
			"last_run_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp of the most recent run",
				Computed:            true,
			},
			"next_run_at": schema.StringAttribute{
				MarkdownDescription: "Timestamp of the next scheduled run",
				Computed:            true,
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

func (r *PipelineResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
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

func (r *PipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data PipelineResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, diags := scheduleConfig(data.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	schedule, err := r.client.Ingestion.CreateSchedule(ctx, marmot.CreateScheduleInput{
		Name:           data.Name.ValueString(),
		PluginID:       data.PluginID.ValueString(),
		Config:         config,
		CronExpression: data.CronExpression.ValueString(),
		Enabled:        data.Enabled.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create pipeline: %s", err))
		return
	}

	if schedule.ID == "" {
		resp.Diagnostics.AddError("API Error", "Pipeline created but no ID returned")
		return
	}

	applyScheduleComputedFields(&data, schedule)

	tflog.Info(ctx, "Pipeline created", map[string]any{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PipelineResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var data PipelineResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	schedule, err := r.client.Ingestion.GetSchedule(ctx, data.ID.ValueString())
	if err != nil {
		if marmot.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read pipeline: %s", err))
		return
	}

	resp.Diagnostics.Append(r.updateModelFromResponse(&data, schedule)...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PipelineResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var data PipelineResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	var state PipelineResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, diags := scheduleConfig(data.Config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	schedule, err := r.client.Ingestion.UpdateSchedule(ctx, state.ID.ValueString(), marmot.UpdateScheduleInput{
		Name:           data.Name.ValueString(),
		PluginID:       data.PluginID.ValueString(),
		Config:         config,
		CronExpression: data.CronExpression.ValueString(),
		Enabled:        data.Enabled.ValueBool(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update pipeline: %s", err))
		return
	}

	applyScheduleComputedFields(&data, schedule)

	tflog.Info(ctx, "Pipeline updated", map[string]any{
		"id":   data.ID.ValueString(),
		"name": data.Name.ValueString(),
	})

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *PipelineResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data PipelineResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Ingestion.DeleteSchedule(ctx, data.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to delete pipeline: %s", err))
		return
	}

	tflog.Info(ctx, "Pipeline deleted", map[string]any{
		"id": data.ID.ValueString(),
	})
}

func (r *PipelineResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}

// scheduleConfig turns the JSON config attribute into the map the SDK expects.
func scheduleConfig(config jsontypes.Normalized) (map[string]any, diag.Diagnostics) {
	if config.IsNull() || config.IsUnknown() {
		return nil, nil
	}
	var out map[string]any
	diags := config.Unmarshal(&out)
	return out, diags
}

// applyScheduleComputedFields copies the server-generated (read-only) attributes
// from an API response onto the model, leaving every configured attribute
// untouched. The configured `config` is kept as written so a plan-time equal
// value never trips an inconsistent-result error after apply.
func applyScheduleComputedFields(model *PipelineResourceModel, schedule *marmot.Schedule) {
	model.ID = types.StringValue(schedule.ID)
	model.Enabled = types.BoolValue(schedule.Enabled)
	model.ManagedBy = types.StringValue(schedule.ManagedBy)
	model.LastRunStatus = types.StringValue(schedule.LastRunStatus)
	model.LastRunAt = types.StringValue(schedule.LastRunAt)
	model.NextRunAt = types.StringValue(schedule.NextRunAt)
	model.CreatedAt = types.StringValue(schedule.CreatedAt)
	model.UpdatedAt = types.StringValue(schedule.UpdatedAt)
}

// updateModelFromResponse refreshes every attribute from the API, including the
// configured ones, so a Read reflects drift made outside Terraform.
func (r *PipelineResource) updateModelFromResponse(model *PipelineResourceModel, schedule *marmot.Schedule) diag.Diagnostics {
	var diags diag.Diagnostics

	model.Name = types.StringValue(schedule.Name)
	model.PluginID = types.StringValue(schedule.PluginID)
	model.CronExpression = types.StringValue(schedule.CronExpression)

	if schedule.Config != nil {
		encoded, err := json.Marshal(schedule.Config)
		if err != nil {
			diags.AddError("Config Error", fmt.Sprintf("Unable to encode pipeline config: %s", err))
			return diags
		}
		model.Config = jsontypes.NewNormalizedValue(string(encoded))
	} else {
		model.Config = jsontypes.NewNormalizedNull()
	}

	applyScheduleComputedFields(model, schedule)
	return diags
}
