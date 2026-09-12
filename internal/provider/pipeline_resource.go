// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-jsontypes/jsontypes"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
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
//
// Create, Read and Update go through the hand-written client because the
// generated SDK does not carry a schedule's secrets and would drop them on
// the way through. Delete has nothing to drop and stays on the SDK.
type PipelineResource struct {
	client  *marmot.Client
	secrets *secretStoreClient
}

// PipelineResourceModel describes the pipeline resource data model.
type PipelineResourceModel struct {
	Name           types.String         `tfsdk:"name"`
	PluginID       types.String         `tfsdk:"plugin_id"`
	Config         jsontypes.Normalized `tfsdk:"config"`
	CronExpression types.String         `tfsdk:"cron_expression"`
	Enabled        types.Bool           `tfsdk:"enabled"`
	Secrets        types.Set            `tfsdk:"secret"`
	ID             types.String         `tfsdk:"id"`
	ManagedBy      types.String         `tfsdk:"managed_by"`
	LastRunStatus  types.String         `tfsdk:"last_run_status"`
	LastRunAt      types.String         `tfsdk:"last_run_at"`
	NextRunAt      types.String         `tfsdk:"next_run_at"`
	CreatedAt      types.String         `tfsdk:"created_at"`
	UpdatedAt      types.String         `tfsdk:"updated_at"`
}

// pipelineSecretModel is one `secret` block.
type pipelineSecretModel struct {
	Key   types.String         `tfsdk:"key"`
	Store types.String         `tfsdk:"store"`
	Ref   jsontypes.Normalized `tfsdk:"ref"`
}

var pipelineSecretType = types.ObjectType{
	AttrTypes: map[string]attr.Type{
		"key":   types.StringType,
		"store": types.StringType,
		"ref":   jsontypes.NormalizedType{},
	},
}

func (r *PipelineResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_pipeline"
}

func (r *PipelineResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A pipeline: a plugin pointed at a source that discovers and catalogs " +
			"assets on a recurring schedule. Rather than declaring each asset by hand, point a plugin " +
			"at a source and Marmot keeps the catalog in sync from what it finds there.\n\n" +
			"Credentials the plugin needs can be kept out of `config` and out of state: a `secret` " +
			"block names a value in a `marmot_secret_store_*` and the key in `config` to inject it " +
			"at, and Marmot resolves it before each run.",

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
					"Use `jsonencode()` to build it from HCL. Leave out any key a `secret` block injects.",
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

		Blocks: map[string]schema.Block{
			"secret": schema.SetNestedBlock{
				MarkdownDescription: "A value resolved from a secret store before each run and injected " +
					"into the plugin config at `key`. Only the reference is stored; the value never " +
					"enters Terraform state or the pipeline's stored config. Registering secrets requires " +
					"the `secretStore:use` permission and Marmot Cloud or Marmot Enterprise.",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"key": schema.StringAttribute{
							MarkdownDescription: "Dot path in the plugin config to inject the value at, " +
								"for example `password` or `credentials.private_key`. Unique per pipeline.",
							Required: true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"store": schema.StringAttribute{
							MarkdownDescription: "ID of the `marmot_secret_store_*` resource holding the secret.",
							Required:            true,
							Validators: []validator.String{
								stringvalidator.LengthAtLeast(1),
							},
						},
						"ref": schema.StringAttribute{
							MarkdownDescription: "Where the secret lives in the store, as a JSON object whose " +
								"keys depend on the store type; see the store resource. Use `jsonencode()` " +
								"to build it from HCL.",
							Required:   true,
							CustomType: jsontypes.NormalizedType{},
						},
					},
				},
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
	r.secrets = newSecretStoreClient(client)
}

func (r *PipelineResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var data PipelineResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	config, diags := scheduleConfig(data.Config)
	resp.Diagnostics.Append(diags...)
	secrets, diags := scheduleSecrets(ctx, data.Secrets)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	schedule, err := r.secrets.CreateSchedule(ctx, createScheduleRequest{
		Name:           data.Name.ValueString(),
		PluginID:       data.PluginID.ValueString(),
		Config:         config,
		CronExpression: data.CronExpression.ValueString(),
		Enabled:        data.Enabled.ValueBool(),
		Secrets:        secrets,
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to create pipeline: %s", err))
		return
	}

	if schedule.ID == "" {
		resp.Diagnostics.AddError("API Error", "Pipeline created but no ID returned")
		return
	}

	applyScheduleComputedFields(&data, &schedule.Schedule)

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

	schedule, err := r.secrets.GetSchedule(ctx, data.ID.ValueString())
	if err != nil {
		if errors.Is(err, errNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read pipeline: %s", err))
		return
	}

	resp.Diagnostics.Append(r.updateModelFromResponse(&data, &schedule.Schedule)...)
	if resp.Diagnostics.HasError() {
		return
	}

	blocks, diags := secretBlocks(ctx, schedule.Secrets)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	data.Secrets = blocks

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
	secrets, diags := scheduleSecrets(ctx, data.Secrets)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The full list goes every time, empty included: the server replaces
	// what it has with what is sent, and only an absent field keeps it.
	schedule, err := r.secrets.UpdateSchedule(ctx, state.ID.ValueString(), updateScheduleRequest{
		Name:           data.Name.ValueString(),
		PluginID:       data.PluginID.ValueString(),
		Config:         config,
		CronExpression: data.CronExpression.ValueString(),
		Enabled:        data.Enabled.ValueBool(),
		Secrets:        secrets,
	})
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to update pipeline: %s", err))
		return
	}

	applyScheduleComputedFields(&data, &schedule.Schedule)

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

	// An object already gone is the outcome Delete wanted, so a 404 here
	// is success. Erroring instead wedges destroy behind a manual state rm.
	if err := r.client.Ingestion.DeleteSchedule(ctx, data.ID.ValueString()); err != nil && !marmot.IsNotFound(err) {
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

// scheduleSecrets turns the secret blocks into the list the API takes. The
// result is never nil: on update an absent list keeps what the server has,
// and only an empty one clears it.
func scheduleSecrets(ctx context.Context, set types.Set) ([]pipelineSecret, diag.Diagnostics) {
	out := []pipelineSecret{}
	if set.IsNull() || set.IsUnknown() {
		return out, nil
	}
	var blocks []pipelineSecretModel
	diags := set.ElementsAs(ctx, &blocks, false)
	if diags.HasError() {
		return nil, diags
	}
	for _, b := range blocks {
		var ref map[string]any
		diags.Append(b.Ref.Unmarshal(&ref)...)
		out = append(out, pipelineSecret{
			Key:           b.Key.ValueString(),
			SecretStoreID: b.Store.ValueString(),
			Ref:           ref,
		})
	}
	return out, diags
}

// secretBlocks turns the API's secrets into the set of blocks. No secrets is
// an empty set, which is how Terraform represents no blocks written.
func secretBlocks(ctx context.Context, secrets []pipelineSecret) (types.Set, diag.Diagnostics) {
	blocks := make([]pipelineSecretModel, 0, len(secrets))
	for _, s := range secrets {
		encoded, err := json.Marshal(s.Ref)
		if err != nil {
			var diags diag.Diagnostics
			diags.AddError("Ref Error", fmt.Sprintf("Unable to encode ref for secret %q: %s", s.Key, err))
			return types.SetNull(pipelineSecretType), diags
		}
		blocks = append(blocks, pipelineSecretModel{
			Key:   types.StringValue(s.Key),
			Store: types.StringValue(s.SecretStoreID),
			Ref:   jsontypes.NewNormalizedValue(string(encoded)),
		})
	}
	return types.SetValueFrom(ctx, pipelineSecretType, blocks)
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
