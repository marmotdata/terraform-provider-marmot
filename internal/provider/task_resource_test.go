// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

const taskUnavailableSummary = "Marmot Tasks Is Not Enabled or Not Available"

// taskFixture is the task resource wired to an in-memory task API.
type taskFixture struct {
	r       *TaskResource
	fake    *fakeTasks
	schema  schema.Schema
	objType tftypes.Object
}

func newTaskFixture(t *testing.T) taskFixture {
	t.Helper()
	fake := newFakeTasks(t)
	r := &TaskResource{client: fake.client()}
	s := schemaOf(t, r)
	objType, ok := s.Type().TerraformType(t.Context()).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is %T, want tftypes.Object", s.Type().TerraformType(t.Context()))
	}
	return taskFixture{r: r, fake: fake, schema: s, objType: objType}
}

func (f taskFixture) nullState() tfsdk.State {
	return tfsdk.State{Schema: f.schema, Raw: tftypes.NewValue(f.objType, nil)}
}

func (f taskFixture) plan(t *testing.T, m taskModel) tfsdk.Plan {
	t.Helper()
	plan := tfsdk.Plan{Schema: f.schema, Raw: tftypes.NewValue(f.objType, nil)}
	if diags := plan.Set(t.Context(), &m); diags.HasError() {
		t.Fatalf("building plan: %v", diags)
	}
	return plan
}

func (f taskFixture) config(t *testing.T, m taskModel) tfsdk.Config {
	t.Helper()
	return tfsdk.Config{Schema: f.schema, Raw: f.plan(t, m).Raw}
}

// create applies a planned task and holds the result to what Terraform
// holds a provider to.
func (f taskFixture) create(t *testing.T, m taskModel) tfsdk.State {
	t.Helper()
	plan := f.plan(t, m)
	resp := resource.CreateResponse{State: f.nullState()}
	f.r.Create(t.Context(), resource.CreateRequest{Plan: plan}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Create: %v", resp.Diagnostics)
	}
	assertApplied(t, plan, resp.State)
	return resp.State
}

func (f taskFixture) update(t *testing.T, prior tfsdk.State, m taskModel) tfsdk.State {
	t.Helper()
	plan := f.plan(t, m)
	resp := resource.UpdateResponse{State: prior}
	f.r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: prior}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Update: %v", resp.Diagnostics)
	}
	assertApplied(t, plan, resp.State)
	return resp.State
}

func (f taskFixture) read(t *testing.T, prior tfsdk.State) tfsdk.State {
	t.Helper()
	resp := resource.ReadResponse{State: prior}
	f.r.Read(t.Context(), resource.ReadRequest{State: prior}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Read: %v", resp.Diagnostics)
	}
	return resp.State
}

// assertApplied fails where Terraform would report "Provider produced
// inconsistent result after apply": a value the plan knew came back
// different, or the new state still holds an unknown.
func assertApplied(t *testing.T, plan tfsdk.Plan, state tfsdk.State) {
	t.Helper()
	if !state.Raw.IsFullyKnown() {
		t.Errorf("state holds an unknown value: %s", state.Raw)
	}
	assertAppliedAt(t, "task", plan.Raw, state.Raw)
}

func assertAppliedAt(t *testing.T, at string, planned, got tftypes.Value) {
	t.Helper()
	if !planned.IsKnown() {
		return
	}
	if planned.IsFullyKnown() {
		if !planned.Equal(got) {
			t.Errorf("%s: planned %s, got %s", at, planned, got)
		}
		return
	}
	switch {
	case planned.Type().Is(tftypes.Object{}):
		var want, have map[string]tftypes.Value
		if err := planned.As(&want); err != nil {
			t.Fatalf("%s: %v", at, err)
		}
		if err := got.As(&have); err != nil || have == nil {
			t.Fatalf("%s: planned an object, got %s (%v)", at, got, err)
		}
		for name, value := range want {
			assertAppliedAt(t, at+"."+name, value, have[name])
		}
	case planned.Type().Is(tftypes.List{}):
		var want, have []tftypes.Value
		if err := planned.As(&want); err != nil {
			t.Fatalf("%s: %v", at, err)
		}
		if err := got.As(&have); err != nil || len(have) != len(want) {
			t.Fatalf("%s: planned %d elements, got %s (%v)", at, len(want), got, err)
		}
		for i, value := range want {
			assertAppliedAt(t, at+"["+strconv.Itoa(i)+"]", value, have[i])
		}
	default:
		t.Fatalf("%s: cannot compare a partly unknown %s", at, planned.Type())
	}
}

var (
	noStrings  = types.SetNull(types.StringType)
	objectAsIs = basetypes.ObjectAsOptions{}
)

// jsonObject reads what the fake holds as decoded JSON.
func jsonObject(t *testing.T, v any) map[string]any {
	t.Helper()
	m, ok := v.(map[string]any)
	if !ok {
		t.Fatalf("%#v is not an object", v)
	}
	return m
}

func stringSetOf(values ...string) types.Set {
	elements := make([]attr.Value, 0, len(values))
	for _, v := range values {
		elements = append(elements, types.StringValue(v))
	}
	return types.SetValueMust(types.StringType, elements)
}

func objectFrom(t *testing.T, attrTypes map[string]attr.Type, v any) types.Object {
	t.Helper()
	obj, diags := types.ObjectValueFrom(t.Context(), attrTypes, v)
	if diags.HasError() {
		t.Fatalf("building object: %v", diags)
	}
	return obj
}

// triggerOf builds a trigger with only its type set.
func triggerOf(t *testing.T, triggerType string, change func(*taskTriggerModel)) types.Object {
	t.Helper()
	m := taskTriggerModel{
		Type:   types.StringValue(triggerType),
		Events: noStrings,
		Filter: types.ObjectNull(taskFilterAttrTypes),
	}
	if change != nil {
		change(&m)
	}
	return objectFrom(t, taskTriggerAttrTypes, m)
}

func filterOf(t *testing.T, assetTypes, providers, pipelines types.Set) types.Object {
	t.Helper()
	return objectFrom(t, taskFilterAttrTypes, taskFilterModel{
		AssetTypes: assetTypes, Providers: providers, Pipelines: pipelines,
	})
}

// limitsOf builds limits as planned: what is not given is for the server.
func limitsOf(t *testing.T, change func(*taskLimitsModel)) types.Object {
	t.Helper()
	m := taskLimitsModel{
		MaxSteps:       types.Int64Unknown(),
		MaxQueries:     types.Int64Unknown(),
		MaxRows:        types.Int64Unknown(),
		MaxTokens:      types.Int64Unknown(),
		TimeoutSeconds: types.Int64Unknown(),
	}
	if change != nil {
		change(&m)
	}
	return objectFrom(t, taskLimitsAttrTypes, m)
}

// plannedTask is the smallest task as Terraform plans its creation: what
// the configuration leaves out is null, what the server decides is unknown.
func plannedTask(t *testing.T, change func(*taskModel)) taskModel {
	t.Helper()
	m := taskModel{
		ID:      types.StringUnknown(),
		Name:    types.StringValue("weekly-revenue"),
		Prompt:  types.StringValue("Report last week's revenue."),
		Enabled: types.BoolValue(true),
		Trigger: triggerOf(t, taskTriggerOnce, nil),
		Sources: noStrings,
		Limits:  types.ObjectUnknown(taskLimitsAttrTypes),
	}
	if change != nil {
		change(&m)
	}
	return serverDecides(m)
}

// serverDecides marks what every write leaves to the server.
func serverDecides(m taskModel) taskModel {
	m.RunAs = types.ObjectUnknown(taskPrincipalAttrTypes)
	m.UpdatedAt = types.StringUnknown()
	if m.CreatedAt.IsNull() {
		m.CreatedAt = types.StringUnknown()
	}
	return m
}

// configuredTask is plannedTask as the configuration has it: nothing the
// server decides is there.
func configuredTask(t *testing.T, change func(*taskModel)) taskModel {
	t.Helper()
	m := plannedTask(t, change)
	m.ID = types.StringNull()
	m.RunAs = types.ObjectNull(taskPrincipalAttrTypes)
	m.CreatedAt = types.StringNull()
	m.UpdatedAt = types.StringNull()
	if m.Limits.IsUnknown() {
		m.Limits = types.ObjectNull(taskLimitsAttrTypes)
	}
	return m
}

// fullTask is a task using most of what a task has.
func fullTask(t *testing.T) taskModel {
	t.Helper()
	return plannedTask(t, func(m *taskModel) {
		m.Description = types.StringValue("Monday revenue digest")
		m.Trigger = triggerOf(t, taskTriggerSchedule, func(trigger *taskTriggerModel) {
			trigger.Cron = types.StringValue("0 8 * * 1")
		})
		m.RunAsServiceAccountID = types.StringValue("sa1")
		m.Sources = stringSetOf("warehouse", "billing")
		m.Limits = limitsOf(t, func(limits *taskLimitsModel) {
			limits.MaxSteps = types.Int64Value(40)
		})
	})
}

func taskIn(t *testing.T, state tfsdk.State) taskModel {
	t.Helper()
	var m taskModel
	if diags := state.Get(t.Context(), &m); diags.HasError() {
		t.Fatalf("reading state: %v", diags)
	}
	return m
}

func triggerIn(t *testing.T, m taskModel) taskTriggerModel {
	t.Helper()
	var out taskTriggerModel
	if diags := m.Trigger.As(t.Context(), &out, objectAsIs); diags.HasError() {
		t.Fatalf("reading trigger: %v", diags)
	}
	return out
}

func limitsIn(t *testing.T, m taskModel) taskLimits {
	t.Helper()
	var out taskLimitsModel
	if diags := m.Limits.As(t.Context(), &out, objectAsIs); diags.HasError() {
		t.Fatalf("reading limits: %v", diags)
	}
	return taskLimits{
		MaxSteps:       out.MaxSteps.ValueInt64(),
		MaxQueries:     out.MaxQueries.ValueInt64(),
		MaxRows:        out.MaxRows.ValueInt64(),
		MaxTokens:      out.MaxTokens.ValueInt64(),
		TimeoutSeconds: out.TimeoutSeconds.ValueInt64(),
	}
}

func runAsIn(t *testing.T, m taskModel) taskPrincipal {
	t.Helper()
	var out taskPrincipalModel
	if diags := m.RunAs.As(t.Context(), &out, objectAsIs); diags.HasError() {
		t.Fatalf("reading run_as: %v", diags)
	}
	return taskPrincipal{Type: out.Type.ValueString(), ID: out.ID.ValueString(), Name: out.Name.ValueString()}
}

// errorSummaries lists the summaries of the error diagnostics.
func errorSummaries(diags diag.Diagnostics) []string {
	var out []string
	for _, d := range diags.Errors() {
		out = append(out, d.Summary())
	}
	return out
}

func TestTaskSchemaIsValid(t *testing.T) {
	r := NewTaskResource()
	if name := typeName(t, r); name != "marmot_task" {
		t.Errorf("type name = %q, want marmot_task", name)
	}
	if diags := schemaOf(t, r).ValidateImplementation(t.Context()); diags.HasError() {
		t.Fatalf("ValidateImplementation: %v", diags)
	}

	registered := 0
	for _, newResource := range New("test")().Resources(t.Context()) {
		if typeName(t, newResource()) == "marmot_task" {
			registered++
		}
	}
	if registered != 1 {
		t.Errorf("the provider registers marmot_task %d times, want once", registered)
	}
}

func TestTaskSchemaShape(t *testing.T) {
	s := schemaOf(t, NewTaskResource())

	if len(s.Blocks) != 0 {
		t.Errorf("blocks = %v, want none", s.Blocks)
	}
	if !strings.HasPrefix(s.MarkdownDescription, taskCloudOnly) {
		t.Error("the description does not open with the Marmot Cloud notice")
	}
	// What a run changes by itself would be a diff on every plan.
	for _, name := range []string{"next_run_at", "last_run", "disabled_reason", "created_by", "updated_by", "delivery"} {
		if _, ok := s.Attributes[name]; ok {
			t.Errorf("%s is in the schema, want it left out", name)
		}
	}

	for _, name := range []string{"name", "prompt"} {
		if a, ok := s.Attributes[name].(schema.StringAttribute); !ok || !a.Required || len(a.Validators) == 0 {
			t.Errorf("%s = %#v, want a required, validated string", name, s.Attributes[name])
		}
	}
	if a, ok := s.Attributes["enabled"].(schema.BoolAttribute); !ok || !a.Optional || !a.Computed || a.Default == nil {
		t.Errorf("enabled = %#v, want optional with a default", s.Attributes["enabled"])
	}
	if a, ok := s.Attributes["id"].(schema.StringAttribute); !ok || !a.Computed || a.Optional || len(a.PlanModifiers) != 1 {
		t.Errorf("id = %#v, want computed and kept from state", s.Attributes["id"])
	}
	if a, ok := s.Attributes["updated_at"].(schema.StringAttribute); !ok || !a.Computed || len(a.PlanModifiers) != 0 {
		t.Errorf("updated_at = %#v, want computed and unknown on every update", s.Attributes["updated_at"])
	}

	// Every update makes the task run as the caller again, so run_as is
	// never planned from state.
	runAs, ok := s.Attributes["run_as"].(schema.SingleNestedAttribute)
	if !ok || !runAs.Computed || runAs.Optional || runAs.Required || len(runAs.PlanModifiers) != 0 {
		t.Fatalf("run_as = %#v, want computed only, without plan modifiers", s.Attributes["run_as"])
	}
	for _, name := range []string{"type", "id", "name"} {
		if a, ok := runAs.Attributes[name].(schema.StringAttribute); !ok || !a.Computed || a.Optional || len(a.PlanModifiers) != 0 {
			t.Errorf("run_as.%s = %#v, want a computed-only string", name, runAs.Attributes[name])
		}
	}

	trigger, ok := s.Attributes["trigger"].(schema.SingleNestedAttribute)
	if !ok || !trigger.Required {
		t.Fatalf("trigger = %#v, want a required nested attribute", s.Attributes["trigger"])
	}
	if a, ok := trigger.Attributes["type"].(schema.StringAttribute); !ok || !a.Required || len(a.Validators) == 0 {
		t.Errorf("trigger.type = %#v, want a required, validated string", trigger.Attributes["type"])
	}
	if a, ok := trigger.Attributes["cooldown_seconds"].(schema.Int64Attribute); !ok || !a.Optional || !a.Computed || len(a.PlanModifiers) != 1 {
		t.Errorf("trigger.cooldown_seconds = %#v, want optional, computed, with its plan modifier", trigger.Attributes["cooldown_seconds"])
	}
	filter, ok := trigger.Attributes["filter"].(schema.SingleNestedAttribute)
	if !ok || !filter.Optional || filter.Computed {
		t.Fatalf("trigger.filter = %#v, want an optional nested attribute", trigger.Attributes["filter"])
	}
	if _, ok := filter.Attributes["cooldown_seconds"]; ok {
		t.Error("cooldown_seconds is inside filter; the contract has it on the trigger")
	}

	limits, ok := s.Attributes["limits"].(schema.SingleNestedAttribute)
	if !ok || !limits.Optional || !limits.Computed {
		t.Fatalf("limits = %#v, want optional and computed", s.Attributes["limits"])
	}
	for _, name := range []string{"max_steps", "max_queries", "max_rows", "max_tokens", "timeout_seconds"} {
		if a, ok := limits.Attributes[name].(schema.Int64Attribute); !ok || !a.Optional || !a.Computed || a.Default != nil {
			t.Errorf("limits.%s = %#v, want optional and computed, the default left to the server", name, limits.Attributes[name])
		}
	}

}

func TestTaskCreate(t *testing.T) {
	f := newTaskFixture(t)

	state := f.create(t, fullTask(t))

	last := f.fake.last()
	if last.method != http.MethodPost || last.path != "/api/v1/tasks" {
		t.Errorf("sent %s %s, want POST /api/v1/tasks", last.method, last.path)
	}
	wantBody := map[string]any{
		"name":                      "weekly-revenue",
		"description":               "Monday revenue digest",
		"prompt":                    "Report last week's revenue.",
		"enabled":                   true,
		"trigger":                   map[string]any{"type": "schedule", "cron": "0 8 * * 1"},
		"run_as_service_account_id": "sa1",
		"sources":                   []any{"billing", "warehouse"},
		"limits":                    map[string]any{"max_steps": float64(40)},
	}
	if body := bodyOf(t, last); !reflect.DeepEqual(body, wantBody) {
		t.Errorf("body = %#v\nwant   %#v", body, wantBody)
	}

	got := taskIn(t, state)
	if got.ID.ValueString() != "t1" {
		t.Errorf("id = %v, want t1", got.ID)
	}
	if got.CreatedAt.ValueString() == "" || got.UpdatedAt.ValueString() == "" {
		t.Errorf("timestamps = %v, %v, want both set", got.CreatedAt, got.UpdatedAt)
	}
	if got.RunAsServiceAccountID.ValueString() != "sa1" {
		t.Errorf("run_as_service_account_id = %v, want it kept as written", got.RunAsServiceAccountID)
	}
	if want := (taskPrincipal{Type: "service_account", ID: "sa1", Name: "name-of-sa1"}); runAsIn(t, got) != want {
		t.Errorf("run_as = %+v, want %+v", runAsIn(t, got), want)
	}
	// The limit that was set, and the server's defaults for the rest.
	wantLimits := taskLimits{MaxSteps: 40, MaxQueries: 20, MaxRows: 200, MaxTokens: 300000, TimeoutSeconds: 600}
	if limits := limitsIn(t, got); limits != wantLimits {
		t.Errorf("limits = %+v, want %+v", limits, wantLimits)
	}
}

// What the configuration leaves out stays null, though the server answers
// with empty strings and empty lists.
func TestTaskCreateMinimal(t *testing.T) {
	f := newTaskFixture(t)

	state := f.create(t, plannedTask(t, nil))

	wantBody := map[string]any{
		"name":        "weekly-revenue",
		"description": "",
		"prompt":      "Report last week's revenue.",
		"enabled":     true,
		"trigger":     map[string]any{"type": "once"},
		"sources":     []any{},
		"limits":      map[string]any{},
	}
	if body := bodyOf(t, f.fake.last()); !reflect.DeepEqual(body, wantBody) {
		t.Errorf("body = %#v\nwant   %#v", body, wantBody)
	}

	got := taskIn(t, state)
	if !got.Description.IsNull() || !got.Sources.IsNull() || !got.RunAsServiceAccountID.IsNull() {
		t.Errorf("description, sources, run_as_service_account_id = %v, %v, %v, want all null",
			got.Description, got.Sources, got.RunAsServiceAccountID)
	}
	trigger := triggerIn(t, got)
	if !trigger.RunAt.IsNull() || !trigger.Cron.IsNull() || !trigger.Events.IsNull() || !trigger.Filter.IsNull() || !trigger.CooldownSeconds.IsNull() {
		t.Errorf("trigger = %+v, want only its type", trigger)
	}
	if want := (taskPrincipal{Type: "user", ID: "u1", Name: "ada"}); runAsIn(t, got) != want {
		t.Errorf("run_as = %+v, want the caller %+v", runAsIn(t, got), want)
	}
	wantLimits := taskLimits{MaxSteps: 25, MaxQueries: 20, MaxRows: 200, MaxTokens: 300000, TimeoutSeconds: 600}
	if limits := limitsIn(t, got); limits != wantLimits {
		t.Errorf("limits = %+v, want the instance defaults %+v", limits, wantLimits)
	}
}

func TestTaskEventTrigger(t *testing.T) {
	events := stringSetOf("ingestion.failed", "asset.schema_changed")

	t.Run("cooldown left to the server", func(t *testing.T) {
		f := newTaskFixture(t)
		state := f.create(t, plannedTask(t, func(m *taskModel) {
			m.Trigger = triggerOf(t, taskTriggerEvent, func(trigger *taskTriggerModel) {
				trigger.Events = events
				trigger.Filter = filterOf(t, stringSetOf("table"), noStrings, noStrings)
				trigger.CooldownSeconds = types.Int64Unknown()
			})
		}))

		want := map[string]any{
			"type":   "event",
			"events": []any{"asset.schema_changed", "ingestion.failed"},
			"filter": map[string]any{"asset_types": []any{"table"}},
		}
		if got := bodyOf(t, f.fake.last())["trigger"]; !reflect.DeepEqual(got, want) {
			t.Errorf("trigger = %#v\nwant      %#v", got, want)
		}
		trigger := triggerIn(t, taskIn(t, state))
		if trigger.CooldownSeconds.ValueInt64() != fakeCooldownDefault {
			t.Errorf("cooldown_seconds = %v, want the server's %d", trigger.CooldownSeconds, fakeCooldownDefault)
		}
	})

	t.Run("cooldown set", func(t *testing.T) {
		f := newTaskFixture(t)
		state := f.create(t, plannedTask(t, func(m *taskModel) {
			m.Trigger = triggerOf(t, taskTriggerEvent, func(trigger *taskTriggerModel) {
				trigger.Events = events
				trigger.CooldownSeconds = types.Int64Value(900)
			})
		}))

		trigger, _ := bodyOf(t, f.fake.last())["trigger"].(map[string]any)
		if trigger["cooldown_seconds"] != float64(900) {
			t.Errorf("sent cooldown_seconds = %v, want 900 on the trigger", trigger["cooldown_seconds"])
		}
		if _, ok := trigger["filter"]; ok {
			t.Errorf("sent filter = %v, want none", trigger["filter"])
		}
		if got := triggerIn(t, taskIn(t, state)); got.CooldownSeconds.ValueInt64() != 900 || !got.Filter.IsNull() {
			t.Errorf("trigger = %+v, want a cooldown of 900 and no filter", got)
		}
	})
}

// A read right after a write changes nothing: not the service account the
// server never echoes, not a run time it answers in UTC, and not whether an
// empty value was written as empty or left out.
func TestTaskReadAfterWriteChangesNothing(t *testing.T) {
	tests := map[string]func(t *testing.T) taskModel{
		"minimal": func(t *testing.T) taskModel { return plannedTask(t, nil) },
		"full":    fullTask,
		"written as empty": func(t *testing.T) taskModel {
			return plannedTask(t, func(m *taskModel) {
				m.Description = types.StringValue("")
				m.Sources = stringSetOf()
				m.Trigger = triggerOf(t, taskTriggerEvent, func(trigger *taskTriggerModel) {
					trigger.Events = stringSetOf("asset.created")
					trigger.Filter = filterOf(t, stringSetOf(), noStrings, noStrings)
					trigger.CooldownSeconds = types.Int64Value(60)
				})
			})
		},
		"an empty filter": func(t *testing.T) taskModel {
			return plannedTask(t, func(m *taskModel) {
				m.Trigger = triggerOf(t, taskTriggerEvent, func(trigger *taskTriggerModel) {
					trigger.Events = stringSetOf("lineage.changed")
					trigger.Filter = filterOf(t, noStrings, noStrings, noStrings)
					trigger.CooldownSeconds = types.Int64Unknown()
				})
			})
		},
		"run at another offset": func(t *testing.T) taskModel {
			return plannedTask(t, func(m *taskModel) {
				m.Trigger = triggerOf(t, taskTriggerOnce, func(trigger *taskTriggerModel) {
					trigger.RunAt = types.StringValue("2026-12-01T09:00:00+01:00")
				})
			})
		},
	}
	for name, build := range tests {
		t.Run(name, func(t *testing.T) {
			f := newTaskFixture(t)
			state := f.create(t, build(t))

			if read := f.read(t, state); !read.Raw.Equal(state.Raw) {
				t.Errorf("read changed the state\nbefore %s\nafter  %s", state.Raw, read.Raw)
			}
		})
	}
}

func TestTaskReadFollowsTheServer(t *testing.T) {
	f := newTaskFixture(t)
	state := f.create(t, fullTask(t))

	f.fake.edit("t1", func(task map[string]any) {
		task["name"] = "monthly-revenue"
		task["enabled"] = false
		task["sources"] = []any{}
		task["trigger"] = map[string]any{"type": "once", "run_at": "2026-12-01T08:00:00Z"}
		task["run_as"] = map[string]any{"type": "user", "id": "u2", "name": "grace"}
		jsonObject(t, task["limits"])["max_steps"] = float64(10)
		task["updated_at"] = "2026-11-23T00:00:00Z"
	})

	got := taskIn(t, f.read(t, state))
	if got.Name.ValueString() != "monthly-revenue" || got.Enabled.ValueBool() {
		t.Errorf("name, enabled = %v, %v, want the server's", got.Name, got.Enabled)
	}
	// Emptied on the server: null, since nothing wrote an empty set.
	if !got.Sources.IsNull() {
		t.Errorf("sources = %v, want null", got.Sources)
	}
	trigger := triggerIn(t, got)
	if trigger.Type.ValueString() != taskTriggerOnce || trigger.RunAt.ValueString() != "2026-12-01T08:00:00Z" || !trigger.Cron.IsNull() {
		t.Errorf("trigger = %+v, want the server's once trigger", trigger)
	}
	if want := (taskPrincipal{Type: "user", ID: "u2", Name: "grace"}); runAsIn(t, got) != want {
		t.Errorf("run_as = %+v, want %+v", runAsIn(t, got), want)
	}
	if limits := limitsIn(t, got); limits.MaxSteps != 10 {
		t.Errorf("max_steps = %d, want 10", limits.MaxSteps)
	}
	if got.UpdatedAt.ValueString() != "2026-11-23T00:00:00Z" {
		t.Errorf("updated_at = %v, want the server's", got.UpdatedAt)
	}
	if got.ID.ValueString() != "t1" || got.RunAsServiceAccountID.ValueString() != "sa1" {
		t.Errorf("id, run_as_service_account_id = %v, %v, want both kept", got.ID, got.RunAsServiceAccountID)
	}
}

func TestTaskReadRemovesAMissingTask(t *testing.T) {
	f := newTaskFixture(t)
	state := f.create(t, plannedTask(t, nil))
	f.fake.remove("t1")

	if read := f.read(t, state); !read.Raw.IsNull() {
		t.Errorf("state = %s, want the task removed", read.Raw)
	}
}

// An update sends the whole task again, and the task then runs as whoever
// made the update.
func TestTaskUpdate(t *testing.T) {
	f := newTaskFixture(t)
	state := f.create(t, plannedTask(t, func(m *taskModel) {
		m.Description = types.StringValue("Monday revenue digest")
		m.Sources = stringSetOf("warehouse")
	}))
	before := taskIn(t, state)
	if want := (taskPrincipal{Type: "user", ID: "u1", Name: "ada"}); runAsIn(t, before) != want {
		t.Fatalf("run_as = %+v, want %+v", runAsIn(t, before), want)
	}

	f.fake.setCaller("service_account", "sa9", "ci")
	plan := serverDecides(before)
	plan.Limits = types.ObjectUnknown(taskLimitsAttrTypes)
	plan.Prompt = types.StringValue("Report last month's revenue.")
	plan.Trigger = triggerOf(t, taskTriggerSchedule, func(trigger *taskTriggerModel) {
		trigger.Cron = types.StringValue("0 8 1 * *")
	})
	state = f.update(t, state, plan)

	last := f.fake.last()
	if last.method != http.MethodPut || last.path != "/api/v1/tasks/t1" {
		t.Errorf("sent %s %s, want PUT /api/v1/tasks/t1", last.method, last.path)
	}
	wantBody := map[string]any{
		"name":        "weekly-revenue",
		"description": "Monday revenue digest",
		"prompt":      "Report last month's revenue.",
		"enabled":     true,
		"trigger":     map[string]any{"type": "schedule", "cron": "0 8 1 * *"},
		"sources":     []any{"warehouse"},
		"limits":      map[string]any{},
	}
	if body := bodyOf(t, last); !reflect.DeepEqual(body, wantBody) {
		t.Errorf("body = %#v\nwant   %#v", body, wantBody)
	}

	after := taskIn(t, state)
	if want := (taskPrincipal{Type: "service_account", ID: "sa9", Name: "ci"}); runAsIn(t, after) != want {
		t.Errorf("run_as = %+v, want the new caller %+v", runAsIn(t, after), want)
	}
	if after.ID.ValueString() != "t1" || f.fake.count() != 1 {
		t.Errorf("id = %v with %d tasks, want the same task changed in place", after.ID, f.fake.count())
	}
	if !after.CreatedAt.Equal(before.CreatedAt) || after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("created_at %v to %v, updated_at %v to %v, want only updated_at to move",
			before.CreatedAt, after.CreatedAt, before.UpdatedAt, after.UpdatedAt)
	}
	if read := f.read(t, state); !read.Raw.Equal(state.Raw) {
		t.Errorf("read changed the state\nbefore %s\nafter  %s", state.Raw, read.Raw)
	}
}

// The server replaces the task with what is sent, so what the
// configuration dropped goes as empty.
func TestTaskUpdateClearsWhatWasRemoved(t *testing.T) {
	f := newTaskFixture(t)
	state := f.create(t, fullTask(t))

	plan := serverDecides(taskIn(t, state))
	plan.Description = types.StringNull()
	plan.RunAsServiceAccountID = types.StringNull()
	plan.Sources = noStrings
	plan.Limits = types.ObjectUnknown(taskLimitsAttrTypes)
	state = f.update(t, state, plan)

	body := bodyOf(t, f.fake.last())
	if body["description"] != "" || !reflect.DeepEqual(body["sources"], []any{}) {
		t.Errorf("body = %#v, want description and sources sent empty", body)
	}
	if _, ok := body["run_as_service_account_id"]; ok {
		t.Errorf("run_as_service_account_id = %v, want it absent", body["run_as_service_account_id"])
	}
	if !reflect.DeepEqual(body["limits"], map[string]any{}) {
		t.Errorf("limits = %#v, want none so the server applies its defaults", body["limits"])
	}

	got := taskIn(t, state)
	if !got.Description.IsNull() || !got.Sources.IsNull() {
		t.Errorf("description, sources = %v, %v, want both null", got.Description, got.Sources)
	}
	if want := (taskPrincipal{Type: "user", ID: "u1", Name: "ada"}); runAsIn(t, got) != want {
		t.Errorf("run_as = %+v, want the caller %+v once no account is named", runAsIn(t, got), want)
	}
	if limits := limitsIn(t, got); limits.MaxSteps != 25 {
		t.Errorf("max_steps = %d, want the instance default again", limits.MaxSteps)
	}
}

func TestTaskDelete(t *testing.T) {
	f := newTaskFixture(t)
	state := f.create(t, plannedTask(t, nil))

	resp := resource.DeleteResponse{State: state}
	f.r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("Delete: %v", resp.Diagnostics)
	}
	if last := f.fake.last(); last.method != http.MethodDelete || last.path != "/api/v1/tasks/t1" {
		t.Errorf("sent %s %s, want DELETE /api/v1/tasks/t1", last.method, last.path)
	}
	if f.fake.count() != 0 {
		t.Errorf("%d tasks left, want none", f.fake.count())
	}

	// Already gone is no error.
	resp = resource.DeleteResponse{State: state}
	f.r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
	if resp.Diagnostics.HasError() {
		t.Errorf("deleting a task that is gone: %v", resp.Diagnostics)
	}
}

// An import knows the id alone. The read that follows fills in the rest,
// except what the server cannot say.
func TestTaskImport(t *testing.T) {
	f := newTaskFixture(t)
	f.create(t, fullTask(t))
	f.create(t, plannedTask(t, func(m *taskModel) {
		m.Name = types.StringValue("one-off")
	}))

	imported := func(t *testing.T, id string) taskModel {
		t.Helper()
		resp := resource.ImportStateResponse{State: f.nullState()}
		f.r.ImportState(t.Context(), resource.ImportStateRequest{ID: id}, &resp)
		if resp.Diagnostics.HasError() {
			t.Fatalf("ImportState: %v", resp.Diagnostics)
		}
		if got := stringAt(t, resp.State, path.Root("id")); got.ValueString() != id {
			t.Fatalf("id = %v, want %s", got, id)
		}
		state := f.read(t, resp.State)
		if !state.Raw.IsFullyKnown() {
			t.Errorf("state holds an unknown value: %s", state.Raw)
		}
		return taskIn(t, state)
	}

	t.Run("full", func(t *testing.T) {
		got := imported(t, "t1")
		if got.Name.ValueString() != "weekly-revenue" || got.Description.ValueString() != "Monday revenue digest" || !got.Enabled.ValueBool() {
			t.Errorf("name, description, enabled = %v, %v, %v", got.Name, got.Description, got.Enabled)
		}
		if !got.Sources.Equal(stringSetOf("warehouse", "billing")) {
			t.Errorf("sources = %v", got.Sources)
		}
		if trigger := triggerIn(t, got); trigger.Type.ValueString() != taskTriggerSchedule || trigger.Cron.ValueString() != "0 8 * * 1" {
			t.Errorf("trigger = %+v", trigger)
		}
		if limits := limitsIn(t, got); limits.MaxSteps != 40 || limits.MaxTokens != 300000 {
			t.Errorf("limits = %+v", limits)
		}
		if want := (taskPrincipal{Type: "service_account", ID: "sa1", Name: "name-of-sa1"}); runAsIn(t, got) != want {
			t.Errorf("run_as = %+v, want %+v", runAsIn(t, got), want)
		}
		// The server reports who the task runs as, not which account a
		// request named.
		if !got.RunAsServiceAccountID.IsNull() {
			t.Errorf("run_as_service_account_id = %v, want null", got.RunAsServiceAccountID)
		}
	})

	t.Run("minimal", func(t *testing.T) {
		got := imported(t, "t2")
		if got.Name.ValueString() != "one-off" {
			t.Errorf("name = %v, want one-off", got.Name)
		}
		if !got.Description.IsNull() || !got.Sources.IsNull() {
			t.Errorf("description, sources = %v, %v, want both null", got.Description, got.Sources)
		}
		if trigger := triggerIn(t, got); !trigger.RunAt.IsNull() || !trigger.Filter.IsNull() || !trigger.CooldownSeconds.IsNull() {
			t.Errorf("trigger = %+v, want only its type", trigger)
		}
	})
}

// An instance where Tasks is not enabled answers 501. That is said as
// such, and a read keeps the task in state.
func TestTaskReportsTasksNotEnabled(t *testing.T) {
	f := newTaskFixture(t)
	state := f.create(t, plannedTask(t, nil))
	f.fake.setDisabled(true)

	plan := f.plan(t, serverDecides(taskIn(t, state)))
	check := func(t *testing.T, diags diag.Diagnostics) {
		t.Helper()
		if got := errorSummaries(diags); !slices.Equal(got, []string{taskUnavailableSummary}) {
			t.Fatalf("errors = %v, want %q", got, taskUnavailableSummary)
		}
		detail := diags.Errors()[0].Detail()
		for _, want := range []string{"501", "not enabled", "Marmot Cloud or Marmot Enterprise"} {
			if !strings.Contains(detail, want) {
				t.Errorf("detail %q does not mention %q", detail, want)
			}
		}
	}

	t.Run("create", func(t *testing.T) {
		resp := resource.CreateResponse{State: f.nullState()}
		f.r.Create(t.Context(), resource.CreateRequest{Plan: f.plan(t, plannedTask(t, nil))}, &resp)
		check(t, resp.Diagnostics)
		if !resp.State.Raw.IsNull() {
			t.Errorf("state = %s, want nothing created", resp.State.Raw)
		}
	})
	t.Run("read", func(t *testing.T) {
		resp := resource.ReadResponse{State: state}
		f.r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
		check(t, resp.Diagnostics)
		if !resp.State.Raw.Equal(state.Raw) {
			t.Errorf("state = %s, want it untouched", resp.State.Raw)
		}
	})
	t.Run("update", func(t *testing.T) {
		resp := resource.UpdateResponse{State: state}
		f.r.Update(t.Context(), resource.UpdateRequest{Plan: plan, State: state}, &resp)
		check(t, resp.Diagnostics)
		if !resp.State.Raw.Equal(state.Raw) {
			t.Errorf("state = %s, want it untouched", resp.State.Raw)
		}
	})
	t.Run("delete", func(t *testing.T) {
		resp := resource.DeleteResponse{State: state}
		f.r.Delete(t.Context(), resource.DeleteRequest{State: state}, &resp)
		check(t, resp.Diagnostics)
	})
}

// Open-source Marmot has no task routes, which a create can tell.
func TestTaskCreateReportsAMissingAPI(t *testing.T) {
	f := newTaskFixture(t)
	f.r = &TaskResource{client: &taskClient{
		api: newTestSecretStoreClient(t, alwaysRespond(http.StatusNotFound, `{"error":"Not found"}`)),
	}}

	resp := resource.CreateResponse{State: f.nullState()}
	f.r.Create(t.Context(), resource.CreateRequest{Plan: f.plan(t, plannedTask(t, nil))}, &resp)
	if got := errorSummaries(resp.Diagnostics); !slices.Equal(got, []string{taskUnavailableSummary}) {
		t.Errorf("errors = %v, want %q", got, taskUnavailableSummary)
	}
}

func TestTaskReportsTheServersRefusal(t *testing.T) {
	f := newTaskFixture(t)
	f.r = &TaskResource{client: &taskClient{
		api: newTestSecretStoreClient(t, alwaysRespond(http.StatusBadRequest, `{"error":"limits.max_steps exceeds the instance ceiling of 50"}`)),
	}}

	resp := resource.CreateResponse{State: f.nullState()}
	f.r.Create(t.Context(), resource.CreateRequest{Plan: f.plan(t, plannedTask(t, nil))}, &resp)
	if got := errorSummaries(resp.Diagnostics); !slices.Equal(got, []string{"Unable to Create Task"}) {
		t.Fatalf("errors = %v, want Unable to Create Task", got)
	}
	if detail := resp.Diagnostics.Errors()[0].Detail(); !strings.Contains(detail, "exceeds the instance ceiling") {
		t.Errorf("detail %q does not carry the server's message", detail)
	}
}

func TestTaskValidateConfig(t *testing.T) {
	trigger := func(triggerType string, change func(*taskTriggerModel)) func(*testing.T, *taskModel) {
		return func(t *testing.T, m *taskModel) {
			m.Trigger = triggerOf(t, triggerType, change)
		}
	}
	tests := []struct {
		name   string
		change func(*testing.T, *taskModel)
		// want is the path of every error.
		want []string
	}{
		{name: "once", change: trigger(taskTriggerOnce, nil)},
		{name: "once at a time", change: trigger(taskTriggerOnce, func(m *taskTriggerModel) {
			m.RunAt = types.StringValue("2026-12-01T08:00:00Z")
		})},
		{name: "once at an unknown time", change: trigger(taskTriggerOnce, func(m *taskTriggerModel) {
			m.RunAt = types.StringUnknown()
		})},
		{name: "once at no timestamp", want: []string{"trigger.run_at"}, change: trigger(taskTriggerOnce, func(m *taskTriggerModel) {
			m.RunAt = types.StringValue("next monday")
		})},
		{name: "once with a cron", want: []string{"trigger.cron"}, change: trigger(taskTriggerOnce, func(m *taskTriggerModel) {
			m.Cron = types.StringValue("0 8 * * 1")
		})},
		{name: "once with event fields", want: []string{"trigger.cooldown_seconds", "trigger.events", "trigger.filter"},
			change: trigger(taskTriggerOnce, func(m *taskTriggerModel) {
				m.Events = stringSetOf("asset.created")
				m.Filter = types.ObjectValueMust(taskFilterAttrTypes, map[string]attr.Value{
					"asset_types": noStrings, "providers": noStrings, "pipelines": noStrings,
				})
				m.CooldownSeconds = types.Int64Value(60)
			})},
		{name: "schedule", change: trigger(taskTriggerSchedule, func(m *taskTriggerModel) {
			m.Cron = types.StringValue("0 8 * * 1")
		})},
		{name: "schedule with an unknown cron", change: trigger(taskTriggerSchedule, func(m *taskTriggerModel) {
			m.Cron = types.StringUnknown()
		})},
		{name: "schedule without a cron", want: []string{"trigger.cron"}, change: trigger(taskTriggerSchedule, nil)},
		{name: "schedule with a run time", want: []string{"trigger.run_at"}, change: trigger(taskTriggerSchedule, func(m *taskTriggerModel) {
			m.Cron = types.StringValue("0 8 * * 1")
			m.RunAt = types.StringValue("2026-12-01T08:00:00Z")
		})},
		{name: "event", change: trigger(taskTriggerEvent, func(m *taskTriggerModel) {
			m.Events = stringSetOf("asset.created")
			m.CooldownSeconds = types.Int64Value(60)
		})},
		{name: "event without events", want: []string{"trigger.events"}, change: trigger(taskTriggerEvent, nil)},
		{name: "event with a cron", want: []string{"trigger.cron"}, change: trigger(taskTriggerEvent, func(m *taskTriggerModel) {
			m.Events = stringSetOf("asset.created")
			m.Cron = types.StringValue("0 8 * * 1")
		})},
		{name: "unknown type", change: func(t *testing.T, m *taskModel) {
			m.Trigger = objectFrom(t, taskTriggerAttrTypes, taskTriggerModel{
				Type:   types.StringUnknown(),
				Cron:   types.StringValue("0 8 * * 1"),
				Events: noStrings,
				Filter: types.ObjectNull(taskFilterAttrTypes),
			})
		}},
		{name: "unknown trigger", change: func(_ *testing.T, m *taskModel) {
			m.Trigger = types.ObjectUnknown(taskTriggerAttrTypes)
		}},
		{name: "every mistake at once", want: []string{"trigger.cron", "trigger.filter", "trigger.run_at", "trigger.run_at"},
			change: trigger(taskTriggerSchedule, func(m *taskTriggerModel) {
				m.RunAt = types.StringValue("next monday")
				m.Filter = types.ObjectValueMust(taskFilterAttrTypes, map[string]attr.Value{
					"asset_types": noStrings, "providers": noStrings, "pipelines": noStrings,
				})
			})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newTaskFixture(t)
			config := f.config(t, configuredTask(t, func(m *taskModel) { tt.change(t, m) }))

			var resp resource.ValidateConfigResponse
			f.r.ValidateConfig(t.Context(), resource.ValidateConfigRequest{Config: config}, &resp)

			var got []string
			for _, d := range resp.Diagnostics.Errors() {
				withPath, ok := d.(diag.DiagnosticWithPath)
				if !ok {
					t.Fatalf("error %q names no attribute", d.Summary())
				}
				got = append(got, withPath.Path().String())
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("errors at %v, want %v: %v", got, tt.want, resp.Diagnostics)
			}
		})
	}
}

// Only an event trigger has a cooldown for the server to fill in.
func TestNullUnlessEventTrigger(t *testing.T) {
	f := newTaskFixture(t)
	at := path.Root("trigger").AtName("cooldown_seconds")

	tests := []struct {
		name        string
		triggerType types.String
		config      types.Int64
		planned     types.Int64
		want        types.Int64
	}{
		{"once, unset", types.StringValue(taskTriggerOnce), types.Int64Null(), types.Int64Unknown(), types.Int64Null()},
		{"schedule, unset", types.StringValue(taskTriggerSchedule), types.Int64Null(), types.Int64Unknown(), types.Int64Null()},
		{"event, unset", types.StringValue(taskTriggerEvent), types.Int64Null(), types.Int64Unknown(), types.Int64Unknown()},
		{"event, set", types.StringValue(taskTriggerEvent), types.Int64Value(120), types.Int64Value(120), types.Int64Value(120)},
		{"unknown type, unset", types.StringUnknown(), types.Int64Null(), types.Int64Unknown(), types.Int64Unknown()},
		{"once, set to an unknown value", types.StringValue(taskTriggerOnce), types.Int64Unknown(), types.Int64Unknown(), types.Int64Unknown()},
		{"once, kept from state", types.StringValue(taskTriggerOnce), types.Int64Null(), types.Int64Null(), types.Int64Null()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := f.plan(t, plannedTask(t, func(m *taskModel) {
				m.Trigger = objectFrom(t, taskTriggerAttrTypes, taskTriggerModel{
					Type:            tt.triggerType,
					Events:          noStrings,
					Filter:          types.ObjectNull(taskFilterAttrTypes),
					CooldownSeconds: tt.planned,
				})
			}))

			resp := planmodifier.Int64Response{PlanValue: tt.planned}
			nullUnlessEventTrigger{}.PlanModifyInt64(t.Context(), planmodifier.Int64Request{
				Path:        at,
				Plan:        plan,
				ConfigValue: tt.config,
				PlanValue:   tt.planned,
			}, &resp)
			if resp.Diagnostics.HasError() {
				t.Fatalf("PlanModifyInt64: %v", resp.Diagnostics)
			}
			if !resp.PlanValue.Equal(tt.want) {
				t.Errorf("planned %v, want %v", resp.PlanValue, tt.want)
			}
		})
	}
}

// The timestamp stays as written while it names the instant the server
// holds, whatever offset or precision the server answers in.
func TestRunAtValue(t *testing.T) {
	tests := []struct {
		name   string
		server string
		prior  types.String
		want   types.String
	}{
		{"none", "", types.StringNull(), types.StringNull()},
		{"dropped by the server", "", types.StringValue("2026-12-01T08:00:00Z"), types.StringNull()},
		{"imported", "2026-12-01T08:00:00Z", types.StringNull(), types.StringValue("2026-12-01T08:00:00Z")},
		{"as written", "2026-12-01T08:00:00Z", types.StringValue("2026-12-01T08:00:00Z"), types.StringValue("2026-12-01T08:00:00Z")},
		{"another offset", "2026-12-01T08:00:00Z", types.StringValue("2026-12-01T09:00:00+01:00"), types.StringValue("2026-12-01T09:00:00+01:00")},
		{"another precision", "2026-12-01T08:00:00.000Z", types.StringValue("2026-12-01T08:00:00Z"), types.StringValue("2026-12-01T08:00:00Z")},
		{"another instant", "2026-12-02T08:00:00Z", types.StringValue("2026-12-01T08:00:00Z"), types.StringValue("2026-12-02T08:00:00Z")},
		{"not a timestamp", "soon", types.StringValue("2026-12-01T08:00:00Z"), types.StringValue("soon")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runAtValue(tt.server, tt.prior); !got.Equal(tt.want) {
				t.Errorf("runAtValue(%q, %v) = %v, want %v", tt.server, tt.prior, got, tt.want)
			}
		})
	}
}
