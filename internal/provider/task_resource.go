// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
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
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	marmot "github.com/marmotdata/marmot/sdk/go"
)

var _ resource.Resource = &TaskResource{}
var _ resource.ResourceWithImportState = &TaskResource{}
var _ resource.ResourceWithValidateConfig = &TaskResource{}

func NewTaskResource() resource.Resource {
	return &TaskResource{}
}

// TaskResource manages a Marmot Task.
type TaskResource struct {
	client *taskClient
}

// taskCloudOnly heads the task page.
const taskCloudOnly = "~> **Requires Marmot Cloud or Marmot Enterprise.** Open-source Marmot " +
	"has no Tasks API, and an instance where Tasks is not enabled refuses every request, " +
	"so this resource fails on apply there.\n\n"

const (
	taskTriggerOnce     = "once"
	taskTriggerSchedule = "schedule"
	taskTriggerEvent    = "event"
)

type taskModel struct {
	ID                    types.String `tfsdk:"id"`
	Name                  types.String `tfsdk:"name"`
	Description           types.String `tfsdk:"description"`
	Prompt                types.String `tfsdk:"prompt"`
	Enabled               types.Bool   `tfsdk:"enabled"`
	Trigger               types.Object `tfsdk:"trigger"`
	RunAsServiceAccountID types.String `tfsdk:"run_as_service_account_id"`
	Sources               types.Set    `tfsdk:"sources"`
	Limits                types.Object `tfsdk:"limits"`
	RunAs                 types.Object `tfsdk:"run_as"`
	CreatedAt             types.String `tfsdk:"created_at"`
	UpdatedAt             types.String `tfsdk:"updated_at"`
}

type taskTriggerModel struct {
	Type            types.String `tfsdk:"type"`
	RunAt           types.String `tfsdk:"run_at"`
	Cron            types.String `tfsdk:"cron"`
	Events          types.Set    `tfsdk:"events"`
	Filter          types.Object `tfsdk:"filter"`
	CooldownSeconds types.Int64  `tfsdk:"cooldown_seconds"`
}

type taskFilterModel struct {
	AssetTypes types.Set `tfsdk:"asset_types"`
	Providers  types.Set `tfsdk:"providers"`
	Pipelines  types.Set `tfsdk:"pipelines"`
}

type taskLimitsModel struct {
	MaxSteps       types.Int64 `tfsdk:"max_steps"`
	MaxQueries     types.Int64 `tfsdk:"max_queries"`
	MaxRows        types.Int64 `tfsdk:"max_rows"`
	MaxTokens      types.Int64 `tfsdk:"max_tokens"`
	TimeoutSeconds types.Int64 `tfsdk:"timeout_seconds"`
}

type taskPrincipalModel struct {
	Type types.String `tfsdk:"type"`
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
}

// The types of the nested attributes, as the schema below declares them.
var (
	taskStringSet = types.SetType{ElemType: types.StringType}

	taskFilterAttrTypes = map[string]attr.Type{
		"asset_types": taskStringSet,
		"providers":   taskStringSet,
		"pipelines":   taskStringSet,
	}
	taskTriggerAttrTypes = map[string]attr.Type{
		"type":             types.StringType,
		"run_at":           types.StringType,
		"cron":             types.StringType,
		"events":           taskStringSet,
		"filter":           types.ObjectType{AttrTypes: taskFilterAttrTypes},
		"cooldown_seconds": types.Int64Type,
	}
	taskLimitsAttrTypes = map[string]attr.Type{
		"max_steps":       types.Int64Type,
		"max_queries":     types.Int64Type,
		"max_rows":        types.Int64Type,
		"max_tokens":      types.Int64Type,
		"timeout_seconds": types.Int64Type,
	}
	taskPrincipalAttrTypes = map[string]attr.Type{
		"type": types.StringType,
		"id":   types.StringType,
		"name": types.StringType,
	}
)

func (r *TaskResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_task"
}

func (r *TaskResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*marmot.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *marmot.Client, got: %T.", req.ProviderData))
		return
	}
	r.client = newTaskClient(client)
}

func (r *TaskResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	stringSet := func(description string) schema.SetAttribute {
		return schema.SetAttribute{
			MarkdownDescription: description,
			Optional:            true,
			ElementType:         types.StringType,
			Validators: []validator.Set{
				setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
			},
		}
	}
	limit := func(description string) schema.Int64Attribute {
		return schema.Int64Attribute{
			MarkdownDescription: description,
			Optional:            true,
			Computed:            true,
			Validators: []validator.Int64{
				int64validator.AtLeast(1),
			},
		}
	}

	resp.Schema = schema.Schema{
		MarkdownDescription: taskCloudOnly + "A Marmot Task: a goal written as a prompt, which an " +
			"agent completes in the background whenever the `trigger` fires. The agent can search the " +
			"catalog, read assets and run governed, read-only queries, and nothing else.\n\n" +
			"A task has no access of its own. It runs as the principal that last saved it: the one " +
			"behind the provider's credential, or the service account named in " +
			"`run_as_service_account_id`. Every apply that changes the task makes it run as that " +
			"principal again, whoever saved it before; `run_as` reports who that is.\n\n" +
			"A `once` trigger without `run_at` runs the task one time, when it is created. Changing " +
			"the task afterwards does not run it again.",

		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the task, unique per instance.",
				Required:            true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthBetween(1, 255),
				},
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "What the task is for.",
				Optional:            true,
			},
			"prompt": schema.StringAttribute{
				MarkdownDescription: "The goal the agent works towards, in plain language, up to " +
					"8000 characters.",
				Required: true,
				Validators: []validator.String{
					stringvalidator.UTF8LengthBetween(1, 8000),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether the task runs when its trigger fires. Defaults to `true`. " +
					"Set to `false` to keep the task but pause it.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(true),
			},
			"trigger": schema.SingleNestedAttribute{
				MarkdownDescription: "When the task runs.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						MarkdownDescription: "`once` runs the task a single time, `schedule` on a cron, " +
							"`event` when the catalog changes.",
						Required: true,
						Validators: []validator.String{
							stringvalidator.OneOf(taskTriggerOnce, taskTriggerSchedule, taskTriggerEvent),
						},
					},
					"run_at": schema.StringAttribute{
						MarkdownDescription: "When a `once` task runs, as an RFC 3339 timestamp such as " +
							"`2026-12-01T08:00:00Z`. Omit to run it when it is created. The server answers in UTC; " +
							"a timestamp with another offset naming the same instant is kept as written.",
						Optional: true,
					},
					"cron": schema.StringAttribute{
						MarkdownDescription: "Five-field cron expression of a `schedule` task, in UTC, " +
							"for example `0 8 * * 1`. Required for `schedule`.",
						Optional: true,
						Validators: []validator.String{
							stringvalidator.LengthAtLeast(1),
						},
					},
					"events": schema.SetAttribute{
						MarkdownDescription: "Catalog events that run an `event` task: `asset.created`, " +
							"`asset.updated`, `asset.schema_changed`, `asset.deleted`, `lineage.changed`, " +
							"`ingestion.completed`, `ingestion.failed`. Required for `event`.",
						Optional:    true,
						ElementType: types.StringType,
						Validators: []validator.Set{
							setvalidator.SizeAtLeast(1),
							setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
						},
					},
					"filter": schema.SingleNestedAttribute{
						MarkdownDescription: "Narrows the events that run an `event` task.",
						Optional:            true,
						Attributes: map[string]schema.Attribute{
							"asset_types": stringSet("Asset types to match, for example `table`."),
							"providers":   stringSet("Asset providers to match, for example `bigquery`."),
							"pipelines":   stringSet("Pipelines to match."),
						},
					},
					"cooldown_seconds": schema.Int64Attribute{
						MarkdownDescription: "Cooldown of an `event` task in seconds, from 60 to 86400. " +
							"Events are debounced and folded into at most one pending run. Omit to " +
							"leave the cooldown to the server.",
						Optional: true,
						Computed: true,
						Validators: []validator.Int64{
							int64validator.Between(60, 86400),
						},
						PlanModifiers: []planmodifier.Int64{
							nullUnlessEventTrigger{},
						},
					},
				},
			},
			"run_as_service_account_id": schema.StringAttribute{
				MarkdownDescription: "ID of a `marmot_service_account` to run the task as, instead of " +
					"the principal behind the provider's credential. Naming one requires the " +
					"`serviceAccount:manage` permission on the organization. The task can reach what " +
					"that account can, and nothing more.",
				Optional: true,
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"sources": stringSet("Queryable sources the agent may query, for example `warehouse`. " +
				"This only narrows what `run_as` may query. Omit to allow every source `run_as` may " +
				"query."),
			"limits": schema.SingleNestedAttribute{
				MarkdownDescription: "Caps on a single run. The server fills in the instance default " +
					"for each limit left out, and the instance sets a ceiling for each. A limit taken " +
					"out of the configuration keeps its value until the task next changes, and then " +
					"goes back to the instance default.",
				Optional: true,
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"max_steps":       limit("Steps the agent may take."),
					"max_queries":     limit("Queries the agent may run."),
					"max_rows":        limit("Rows per query."),
					"max_tokens":      limit("Model tokens the run may use."),
					"timeout_seconds": limit("Wall-clock time the run may take, in seconds."),
				},
			},
			"id": schema.StringAttribute{
				MarkdownDescription: "Task ID",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			// No plan modifier: every update makes the task run as the caller
			// again, so the prior value says nothing about the next one.
			"run_as": schema.SingleNestedAttribute{
				MarkdownDescription: "The principal the task runs as: whoever last saved it, or the " +
					"service account named in `run_as_service_account_id`.",
				Computed: true,
				Attributes: map[string]schema.Attribute{
					"type": schema.StringAttribute{
						MarkdownDescription: "`user` or `service_account`.",
						Computed:            true,
					},
					"id": schema.StringAttribute{
						MarkdownDescription: "ID of the principal.",
						Computed:            true,
					},
					"name": schema.StringAttribute{
						MarkdownDescription: "Name of the principal.",
						Computed:            true,
					},
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

// nullUnlessEventTrigger plans an unset cooldown as null on a trigger that is
// not an event trigger. Only an event trigger has a cooldown for the server
// to fill in, so anywhere else "known after apply" would be noise.
type nullUnlessEventTrigger struct{}

var _ planmodifier.Int64 = nullUnlessEventTrigger{}

func (m nullUnlessEventTrigger) Description(ctx context.Context) string {
	return m.MarkdownDescription(ctx)
}

func (nullUnlessEventTrigger) MarkdownDescription(context.Context) string {
	return "Plans null when unset on a trigger that is not an event trigger."
}

func (nullUnlessEventTrigger) PlanModifyInt64(ctx context.Context, req planmodifier.Int64Request, resp *planmodifier.Int64Response) {
	if !req.PlanValue.IsUnknown() || !req.ConfigValue.IsNull() {
		return
	}
	var triggerType types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, req.Path.ParentPath().AtName("type"), &triggerType)...)
	if resp.Diagnostics.HasError() || triggerType.IsNull() || triggerType.IsUnknown() {
		return
	}
	if triggerType.ValueString() != taskTriggerEvent {
		resp.PlanValue = types.Int64Null()
	}
}

// isSet reports whether a value is neither null nor unknown.
func isSet(v attr.Value) bool {
	return !v.IsNull() && !v.IsUnknown()
}

func (r *TaskResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var config taskModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	validateTaskTrigger(ctx, config.Trigger, &resp.Diagnostics)
}

// validateTaskTrigger checks that a trigger carries the fields of its type
// and no others. An unknown value may still turn out valid, so it passes.
func validateTaskTrigger(ctx context.Context, obj types.Object, diags *diag.Diagnostics) {
	if !isSet(obj) {
		return
	}
	var trigger taskTriggerModel
	if d := obj.As(ctx, &trigger, basetypes.ObjectAsOptions{}); d.HasError() {
		diags.Append(d...)
		return
	}
	root := path.Root("trigger")

	if isSet(trigger.RunAt) {
		if _, err := time.Parse(time.RFC3339, trigger.RunAt.ValueString()); err != nil {
			diags.AddAttributeError(root.AtName("run_at"), "Invalid Run Time",
				fmt.Sprintf("run_at must be an RFC 3339 timestamp such as 2026-12-01T08:00:00Z: %s", err))
		}
	}

	if !isSet(trigger.Type) {
		return
	}
	triggerType := trigger.Type.ValueString()
	fields := []struct {
		name     string
		value    attr.Value
		owner    string
		required bool
	}{
		{"run_at", trigger.RunAt, taskTriggerOnce, false},
		{"cron", trigger.Cron, taskTriggerSchedule, true},
		{"events", trigger.Events, taskTriggerEvent, true},
		{"filter", trigger.Filter, taskTriggerEvent, false},
		{"cooldown_seconds", trigger.CooldownSeconds, taskTriggerEvent, false},
	}
	for _, f := range fields {
		switch {
		case triggerType == f.owner && f.required && f.value.IsNull():
			diags.AddAttributeError(root.AtName(f.name), "Missing Trigger Attribute",
				fmt.Sprintf("%s is required when the trigger type is %q.", f.name, f.owner))
		case triggerType != f.owner && !f.value.IsNull():
			diags.AddAttributeError(root.AtName(f.name), "Invalid Trigger Attribute",
				fmt.Sprintf("%s only applies when the trigger type is %q, and this trigger is %q.",
					f.name, f.owner, triggerType))
		}
	}
}

// stringsOf reads a set of strings, sorted so a request is deterministic.
// Null and unknown read as nil.
func stringsOf(ctx context.Context, set types.Set, diags *diag.Diagnostics) []string {
	if !isSet(set) {
		return nil
	}
	var out []string
	diags.Append(set.ElementsAs(ctx, &out, false)...)
	slices.Sort(out)
	return out
}

// known reads an integer that is set, zero otherwise.
func known(v types.Int64) int64 {
	if !isSet(v) {
		return 0
	}
	return v.ValueInt64()
}

// taskRequestFrom builds the body of a create or an update from the plan.
// What the plan leaves null or unknown is left out, so the server applies
// its own defaults.
func taskRequestFrom(ctx context.Context, plan taskModel) (taskRequest, diag.Diagnostics) {
	var diags diag.Diagnostics

	in := taskRequest{
		Name:                  plan.Name.ValueString(),
		Description:           plan.Description.ValueString(),
		Prompt:                plan.Prompt.ValueString(),
		Enabled:               plan.Enabled.ValueBool(),
		RunAsServiceAccountID: plan.RunAsServiceAccountID.ValueString(),
		Sources:               stringsOf(ctx, plan.Sources, &diags),
	}
	if in.Sources == nil {
		in.Sources = []string{}
	}

	if isSet(plan.Trigger) {
		var trigger taskTriggerModel
		diags.Append(plan.Trigger.As(ctx, &trigger, basetypes.ObjectAsOptions{})...)
		in.Trigger = taskTrigger{
			Type:            trigger.Type.ValueString(),
			RunAt:           trigger.RunAt.ValueString(),
			Cron:            trigger.Cron.ValueString(),
			Events:          stringsOf(ctx, trigger.Events, &diags),
			CooldownSeconds: known(trigger.CooldownSeconds),
		}
		if isSet(trigger.Filter) {
			var filter taskFilterModel
			diags.Append(trigger.Filter.As(ctx, &filter, basetypes.ObjectAsOptions{})...)
			in.Trigger.Filter = &taskFilter{
				AssetTypes: stringsOf(ctx, filter.AssetTypes, &diags),
				Providers:  stringsOf(ctx, filter.Providers, &diags),
				Pipelines:  stringsOf(ctx, filter.Pipelines, &diags),
			}
		}
	}

	if isSet(plan.Limits) {
		var limits taskLimitsModel
		diags.Append(plan.Limits.As(ctx, &limits, basetypes.ObjectAsOptions{})...)
		in.Limits = taskLimits{
			MaxSteps:       known(limits.MaxSteps),
			MaxQueries:     known(limits.MaxQueries),
			MaxRows:        known(limits.MaxRows),
			MaxTokens:      known(limits.MaxTokens),
			TimeoutSeconds: known(limits.TimeoutSeconds),
		}
	}

	return in, diags
}

// stringOr maps a string the server may leave empty onto its attribute.
// The server does not tell `""` from absent, so empty reads as null unless
// prior was written as `""`.
func stringOr(value string, prior types.String) types.String {
	if value == "" && (!isSet(prior) || prior.ValueString() != "") {
		return types.StringNull()
	}
	return types.StringValue(value)
}

// setOr does the same for a set: empty reads as null unless prior was
// written as an empty set.
func setOr(ctx context.Context, values []string, prior types.Set, diags *diag.Diagnostics) types.Set {
	if len(values) == 0 {
		if !isSet(prior) || len(prior.Elements()) != 0 {
			return types.SetNull(types.StringType)
		}
		return types.SetValueMust(types.StringType, nil)
	}
	set, d := types.SetValueFrom(ctx, types.StringType, values)
	diags.Append(d...)
	return set
}

// runAtValue keeps the timestamp as written while it names the instant the
// server holds, so another offset or precision is not a change.
func runAtValue(value string, prior types.String) types.String {
	if value == "" {
		return types.StringNull()
	}
	if isSet(prior) {
		held, errHeld := time.Parse(time.RFC3339, value)
		written, errWritten := time.Parse(time.RFC3339, prior.ValueString())
		if errHeld == nil && errWritten == nil && held.Equal(written) {
			return prior
		}
	}
	return types.StringValue(value)
}

// taskModelFrom maps a task as the server holds it onto the model. prior is
// the plan after a write and the state on a read: it settles what the server
// cannot say, which is the service account that was named and whether an
// empty value was written as empty or left out.
func taskModelFrom(ctx context.Context, t *task, prior taskModel) (taskModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	m := taskModel{
		ID:                    types.StringValue(t.ID),
		Name:                  types.StringValue(t.Name),
		Description:           stringOr(t.Description, prior.Description),
		Prompt:                types.StringValue(t.Prompt),
		Enabled:               types.BoolValue(t.Enabled),
		RunAsServiceAccountID: prior.RunAsServiceAccountID,
		Sources:               setOr(ctx, t.Sources, prior.Sources, &diags),
		CreatedAt:             types.StringValue(t.CreatedAt),
		UpdatedAt:             types.StringValue(t.UpdatedAt),
	}

	var d diag.Diagnostics
	m.Trigger, d = taskTriggerValue(ctx, t.Trigger, prior.Trigger)
	diags.Append(d...)

	m.Limits, d = types.ObjectValueFrom(ctx, taskLimitsAttrTypes, taskLimitsModel{
		MaxSteps:       types.Int64Value(t.Limits.MaxSteps),
		MaxQueries:     types.Int64Value(t.Limits.MaxQueries),
		MaxRows:        types.Int64Value(t.Limits.MaxRows),
		MaxTokens:      types.Int64Value(t.Limits.MaxTokens),
		TimeoutSeconds: types.Int64Value(t.Limits.TimeoutSeconds),
	})
	diags.Append(d...)

	m.RunAs, d = types.ObjectValueFrom(ctx, taskPrincipalAttrTypes, taskPrincipalModel{
		Type: types.StringValue(t.RunAs.Type),
		ID:   types.StringValue(t.RunAs.ID),
		Name: types.StringValue(t.RunAs.Name),
	})
	diags.Append(d...)

	return m, diags
}

func taskTriggerValue(ctx context.Context, t taskTrigger, prior types.Object) (types.Object, diag.Diagnostics) {
	var diags diag.Diagnostics

	var was taskTriggerModel
	if isSet(prior) {
		diags.Append(prior.As(ctx, &was, basetypes.ObjectAsOptions{})...)
	}

	trigger := taskTriggerModel{
		Type:            types.StringValue(t.Type),
		RunAt:           runAtValue(t.RunAt, was.RunAt),
		Cron:            stringOr(t.Cron, was.Cron),
		Events:          setOr(ctx, t.Events, was.Events, &diags),
		CooldownSeconds: types.Int64Null(),
	}
	// Only an event trigger has a cooldown, whatever the server echoes.
	if t.Type == taskTriggerEvent && t.CooldownSeconds != 0 {
		trigger.CooldownSeconds = types.Int64Value(t.CooldownSeconds)
	}

	var held taskFilter
	if t.Filter != nil {
		held = *t.Filter
	}
	if len(held.AssetTypes)+len(held.Providers)+len(held.Pipelines) == 0 && !isSet(was.Filter) {
		// An empty filter the configuration never wrote.
		trigger.Filter = types.ObjectNull(taskFilterAttrTypes)
	} else {
		var wasFilter taskFilterModel
		if isSet(was.Filter) {
			diags.Append(was.Filter.As(ctx, &wasFilter, basetypes.ObjectAsOptions{})...)
		}
		var d diag.Diagnostics
		trigger.Filter, d = types.ObjectValueFrom(ctx, taskFilterAttrTypes, taskFilterModel{
			AssetTypes: setOr(ctx, held.AssetTypes, wasFilter.AssetTypes, &diags),
			Providers:  setOr(ctx, held.Providers, wasFilter.Providers, &diags),
			Pipelines:  setOr(ctx, held.Pipelines, wasFilter.Pipelines, &diags),
		})
		diags.Append(d...)
	}

	out, d := types.ObjectValueFrom(ctx, taskTriggerAttrTypes, trigger)
	diags.Append(d...)
	return out, diags
}

// addTaskError reports a failed request. A missing Tasks API gets its own
// summary so it does not read as a fault in the task.
func addTaskError(diags *diag.Diagnostics, action string, err error) {
	var unavailable *errNoTaskAPI
	if errors.As(err, &unavailable) {
		diags.AddError("Marmot Tasks Is Not Enabled or Not Available", err.Error())
		return
	}
	diags.AddError("Unable to "+action+" Task", err.Error())
}

func (r *TaskResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan taskModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := taskRequestFrom(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateTask(ctx, in)
	if err != nil {
		addTaskError(&resp.Diagnostics, "Create", err)
		return
	}
	if created.ID == "" {
		resp.Diagnostics.AddError("Unable to Create Task", "The server returned a task without an ID.")
		return
	}

	tflog.Info(ctx, "Task created", map[string]any{
		"id":     created.ID,
		"name":   created.Name,
		"run_as": created.RunAs.Type + ":" + created.RunAs.ID,
	})

	state, diags := taskModelFrom(ctx, created, plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *TaskResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var prior taskModel
	resp.Diagnostics.Append(req.State.Get(ctx, &prior)...)
	if resp.Diagnostics.HasError() {
		return
	}

	found, err := r.client.GetTask(ctx, prior.ID.ValueString())
	if err != nil {
		if errors.Is(err, errNotFound) {
			resp.State.RemoveResource(ctx)
			return
		}
		addTaskError(&resp.Diagnostics, "Read", err)
		return
	}

	state, diags := taskModelFrom(ctx, found, prior)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *TaskResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan taskModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	in, diags := taskRequestFrom(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// The whole task goes every time: the server replaces what it has with
	// what is sent, and makes the task run as the caller again.
	updated, err := r.client.UpdateTask(ctx, id.ValueString(), in)
	if err != nil {
		addTaskError(&resp.Diagnostics, "Update", err)
		return
	}

	tflog.Info(ctx, "Task updated", map[string]any{
		"id":     updated.ID,
		"name":   updated.Name,
		"run_as": updated.RunAs.Type + ":" + updated.RunAs.ID,
	})

	state, diags := taskModelFrom(ctx, updated, plan)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *TaskResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var id types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &id)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Already gone is fine.
	if err := r.client.DeleteTask(ctx, id.ValueString()); err != nil && !errors.Is(err, errNotFound) {
		addTaskError(&resp.Diagnostics, "Delete", err)
		return
	}

	tflog.Info(ctx, "Task deleted", map[string]any{
		"id": id.ValueString(),
	})
}

func (r *TaskResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
