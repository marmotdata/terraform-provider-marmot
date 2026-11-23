// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/marmotdata/marmot/sdk/go/auth"
)

// fakeTasks is the task API held in memory. It works on plain JSON, not on
// the client's types, so a wrong field name in those shows. It answers the
// way the contract allows at its least convenient: every field present,
// the empty ones included, and timestamps in UTC. Like the server, it
// refuses a request with a field it does not know.
type fakeTasks struct {
	t *testing.T

	mu sync.Mutex
	// disabled answers 501 on every route, as an instance without Tasks does.
	disabled bool
	// caller is the principal behind the credential.
	caller   map[string]any
	tasks    map[string]map[string]any
	seq      int
	requests []recordedRequest
}

func newFakeTasks(t *testing.T) *fakeTasks {
	t.Helper()
	return &fakeTasks{
		t:      t,
		caller: map[string]any{"type": "user", "id": "u1", "name": "ada"},
		tasks:  map[string]map[string]any{},
	}
}

// client returns a taskClient pointed at the fake.
func (f *fakeTasks) client() *taskClient {
	f.t.Helper()
	srv := httptest.NewServer(f)
	f.t.Cleanup(srv.Close)
	return &taskClient{api: &secretStoreClient{host: srv.URL, cred: auth.APIKey("test"), http: srv.Client()}}
}

func (f *fakeTasks) setDisabled(disabled bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.disabled = disabled
}

func (f *fakeTasks) setCaller(kind, id, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.caller = map[string]any{"type": kind, "id": id, "name": name}
}

// edit changes a stored task behind the provider's back.
func (f *fakeTasks) edit(id string, change func(task map[string]any)) {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	stored, ok := f.tasks[id]
	if !ok {
		f.t.Fatalf("no task %s to edit", id)
	}
	change(stored)
}

func (f *fakeTasks) remove(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.tasks, id)
}

func (f *fakeTasks) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.tasks)
}

// last returns the most recent request.
func (f *fakeTasks) last() *recordedRequest {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		f.t.Fatal("the server saw no request")
	}
	last := f.requests[len(f.requests)-1]
	return &last
}

func (f *fakeTasks) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		f.t.Errorf("reading request body: %v", err)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, recordedRequest{
		method: r.Method,
		path:   r.URL.Path,
		apiKey: r.Header.Get("X-API-Key"),
		body:   string(payload),
	})

	respond := func(status int, body any) {
		w.WriteHeader(status)
		if body == nil {
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			f.t.Errorf("encoding response: %v", err)
		}
	}
	fail := func(status int, message string) {
		respond(status, map[string]any{"error": message})
	}

	if f.disabled {
		fail(http.StatusNotImplemented, "Tasks is not enabled")
		return
	}

	var in map[string]any
	if len(payload) > 0 {
		if err := json.Unmarshal(payload, &in); err != nil {
			fail(http.StatusBadRequest, "invalid request body")
			return
		}
		if name := unknownTaskField(in); name != "" {
			fail(http.StatusBadRequest, fmt.Sprintf("json: unknown field %q", name))
			return
		}
	}

	id, one := strings.CutPrefix(r.URL.Path, "/api/v1/tasks/")
	stored, found := f.tasks[id]
	switch {
	case r.URL.Path == "/api/v1/tasks" && r.Method == http.MethodPost:
		f.seq++
		created := "t" + strconv.Itoa(f.seq)
		f.tasks[created] = f.save(created, in, nil)
		respond(http.StatusCreated, f.tasks[created])
	case !one || !found:
		fail(http.StatusNotFound, "task not found")
	case r.Method == http.MethodGet:
		respond(http.StatusOK, stored)
	case r.Method == http.MethodPut:
		f.seq++
		f.tasks[id] = f.save(id, in, stored)
		respond(http.StatusOK, f.tasks[id])
	case r.Method == http.MethodDelete:
		delete(f.tasks, id)
		respond(http.StatusNoContent, nil)
	default:
		fail(http.StatusMethodNotAllowed, "method not allowed")
	}
}

// taskInputFields are the fields a create or an update may carry, as the
// server's decoder, which disallows unknown fields, knows them.
var taskInputFields = map[string]map[string]bool{
	"": {"name": true, "description": true, "prompt": true, "enabled": true, "trigger": true,
		"run_as_service_account_id": true, "sources": true, "limits": true},
	"trigger": {"type": true, "run_at": true, "cron": true, "events": true, "filter": true, "cooldown_seconds": true},
	"filter":  {"asset_types": true, "providers": true, "pipelines": true},
	"limits":  {"max_steps": true, "max_queries": true, "max_rows": true, "max_tokens": true, "timeout_seconds": true},
}

// unknownTaskField returns the first field of a request the server would
// refuse, or "".
func unknownTaskField(in map[string]any) string {
	check := func(object any, kind string) string {
		fields, _ := object.(map[string]any)
		for name := range fields {
			if !taskInputFields[kind][name] {
				return name
			}
		}
		return ""
	}
	trigger, _ := in["trigger"].(map[string]any)
	for _, name := range []string{check(in, ""), check(trigger, "trigger"), check(trigger["filter"], "filter"), check(in["limits"], "limits")} {
		if name != "" {
			return name
		}
	}
	return ""
}

// utc answers a timestamp in UTC, as the server does.
func utc(value string) string {
	at, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return at.UTC().Format(time.RFC3339)
}

// fakeLimitDefaults are the instance defaults of the fake.
var fakeLimitDefaults = map[string]any{
	"max_steps":       float64(25),
	"max_queries":     float64(20),
	"max_rows":        float64(200),
	"max_tokens":      float64(300000),
	"timeout_seconds": float64(600),
}

// fakeCooldownDefault is what the fake gives an event trigger without one.
const fakeCooldownDefault = 300

// save turns a request into the task the server holds: a full replace that
// makes the task run as the caller again, or as the named service account.
func (f *fakeTasks) save(id string, in, prior map[string]any) map[string]any {
	now := fmt.Sprintf("2026-11-22T10:00:%02dZ", f.seq)
	var created any = now
	createdBy := f.caller["id"]
	if prior != nil {
		created, createdBy = prior["created_at"], prior["created_by"]
	}

	list := func(v any) []any {
		if items, ok := v.([]any); ok {
			return items
		}
		return []any{}
	}
	text := func(v any) string {
		s, _ := v.(string)
		return s
	}
	object := func(v any) map[string]any {
		if m, ok := v.(map[string]any); ok {
			return m
		}
		return map[string]any{}
	}

	enabled := true
	if v, ok := in["enabled"].(bool); ok {
		enabled = v
	}

	given := object(in["trigger"])
	filter := object(given["filter"])
	trigger := map[string]any{
		"type":   text(given["type"]),
		"run_at": utc(text(given["run_at"])),
		"cron":   text(given["cron"]),
		"events": list(given["events"]),
		"filter": map[string]any{
			"asset_types": list(filter["asset_types"]),
			"providers":   list(filter["providers"]),
			"pipelines":   list(filter["pipelines"]),
		},
		"cooldown_seconds": float64(0),
	}
	if trigger["type"] == "event" {
		trigger["cooldown_seconds"] = float64(fakeCooldownDefault)
		if v, ok := given["cooldown_seconds"].(float64); ok && v != 0 {
			trigger["cooldown_seconds"] = v
		}
	}

	runAs := f.caller
	if account := text(in["run_as_service_account_id"]); account != "" {
		runAs = map[string]any{"type": "service_account", "id": account, "name": "name-of-" + account}
	}

	limits := map[string]any{}
	for name, fallback := range fakeLimitDefaults {
		limits[name] = fallback
		if v, ok := object(in["limits"])[name].(float64); ok && v != 0 {
			limits[name] = v
		}
	}

	return map[string]any{
		"id":              id,
		"name":            text(in["name"]),
		"description":     text(in["description"]),
		"prompt":          text(in["prompt"]),
		"enabled":         enabled,
		"disabled_reason": "",
		"trigger":         trigger,
		"run_as":          runAs,
		"sources":         list(in["sources"]),
		"limits":          limits,
		"next_run_at":     now,
		"last_run":        nil,
		"created_by":      createdBy,
		"created_at":      created,
		"updated_by":      f.caller["id"],
		"updated_at":      now,
	}
}

func TestTaskClientRoutes(t *testing.T) {
	fake := newFakeTasks(t)
	c := fake.client()

	in := taskRequest{
		Name:    "weekly-revenue",
		Prompt:  "Report last week's revenue.",
		Enabled: true,
		Trigger: taskTrigger{Type: "schedule", Cron: "0 8 * * 1"},
		Sources: []string{"warehouse"},
	}

	created, err := c.CreateTask(t.Context(), in)
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if last := fake.last(); last.method != http.MethodPost || last.path != "/api/v1/tasks" || last.apiKey != "test" {
		t.Errorf("create sent %+v, want POST /api/v1/tasks with the credential", last)
	}
	if created.ID != "t1" || created.Name != in.Name || created.Trigger.Cron != in.Trigger.Cron {
		t.Errorf("created = %+v", created)
	}
	if created.RunAs != (taskPrincipal{Type: "user", ID: "u1", Name: "ada"}) {
		t.Errorf("run_as = %+v, want the caller", created.RunAs)
	}
	if created.Limits.MaxSteps != 25 || created.Limits.TimeoutSeconds != 600 {
		t.Errorf("limits = %+v, want the instance defaults", created.Limits)
	}

	got, err := c.GetTask(t.Context(), created.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if last := fake.last(); last.method != http.MethodGet || last.path != "/api/v1/tasks/t1" {
		t.Errorf("get sent %s %s, want GET /api/v1/tasks/t1", last.method, last.path)
	}
	if got.Prompt != in.Prompt || len(got.Sources) != 1 || got.Sources[0] != "warehouse" {
		t.Errorf("got = %+v", got)
	}

	in.Prompt = "Report last month's revenue."
	updated, err := c.UpdateTask(t.Context(), created.ID, in)
	if err != nil {
		t.Fatalf("UpdateTask: %v", err)
	}
	if last := fake.last(); last.method != http.MethodPut || last.path != "/api/v1/tasks/t1" {
		t.Errorf("update sent %s %s, want PUT /api/v1/tasks/t1", last.method, last.path)
	}
	if updated.Prompt != in.Prompt || updated.CreatedAt != created.CreatedAt || updated.UpdatedAt == created.UpdatedAt {
		t.Errorf("updated = %+v", updated)
	}

	if err := c.DeleteTask(t.Context(), created.ID); err != nil {
		t.Fatalf("DeleteTask: %v", err)
	}
	if last := fake.last(); last.method != http.MethodDelete || last.path != "/api/v1/tasks/t1" {
		t.Errorf("delete sent %s %s, want DELETE /api/v1/tasks/t1", last.method, last.path)
	}
	if _, err := c.GetTask(t.Context(), created.ID); !errors.Is(err, errNotFound) {
		t.Errorf("get after delete = %v, want errNotFound", err)
	}
}

// An update replaces the whole task, so what is empty goes as empty, not
// as absent. What the server defaults is left out.
func TestTaskRequestWireFormat(t *testing.T) {
	encoded, err := json.Marshal(taskRequest{
		Name:    "t",
		Prompt:  "p",
		Trigger: taskTrigger{Type: "once"},
	})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal(encoded, &body); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	for _, name := range []string{"name", "description", "prompt", "enabled", "trigger", "sources", "limits"} {
		if _, ok := body[name]; !ok {
			t.Errorf("%s is absent, want it sent", name)
		}
	}
	if _, ok := body["run_as_service_account_id"]; ok {
		t.Errorf("run_as_service_account_id = %v, want it absent when no account is named", body["run_as_service_account_id"])
	}
	if body["enabled"] != false {
		t.Errorf("enabled = %v, want an explicit false", body["enabled"])
	}
	trigger, _ := body["trigger"].(map[string]any)
	if len(trigger) != 1 || trigger["type"] != "once" {
		t.Errorf("trigger = %v, want only its type", trigger)
	}
	if limits, _ := body["limits"].(map[string]any); len(limits) != 0 {
		t.Errorf("limits = %v, want none so the server applies its defaults", limits)
	}
	// The server refuses a field it does not know, and tasks no longer
	// have destinations.
	if _, ok := body["delivery"]; ok {
		t.Errorf("delivery = %v, want it absent", body["delivery"])
	}
}

// An instance where Tasks is not enabled answers 501 on every task route.
func TestTaskClientReportsTasksNotEnabled(t *testing.T) {
	fake := newFakeTasks(t)
	fake.setDisabled(true)
	c := fake.client()

	calls := map[string]func() error{
		"create": func() error {
			_, err := c.CreateTask(t.Context(), taskRequest{Name: "t"})
			return err
		},
		"get": func() error {
			_, err := c.GetTask(t.Context(), "t1")
			return err
		},
		"update": func() error {
			_, err := c.UpdateTask(t.Context(), "t1", taskRequest{Name: "t"})
			return err
		},
		"delete": func() error {
			return c.DeleteTask(t.Context(), "t1")
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			var unavailable *errNoTaskAPI
			if !errors.As(err, &unavailable) {
				t.Fatalf("got %v, want *errNoTaskAPI", err)
			}
			if errors.Is(err, errNotFound) {
				t.Error("a 501 matches errNotFound, so a read would drop the task from state")
			}
			for _, want := range []string{c.api.host, "501", "not enabled", "Marmot Cloud or Marmot Enterprise"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// Open-source Marmot has no task routes. A 404 on POST can only be the
// route missing, while a 404 on a task's own path may be the task gone.
func TestTaskClientTellsAMissingAPIFromAMissingTask(t *testing.T) {
	c := &taskClient{api: newTestSecretStoreClient(t, alwaysRespond(http.StatusNotFound, `{"error":"Not found"}`))}

	var unavailable *errNoTaskAPI
	if _, err := c.CreateTask(t.Context(), taskRequest{Name: "t"}); !errors.As(err, &unavailable) {
		t.Errorf("create = %v, want *errNoTaskAPI", err)
	} else if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %q does not mention the status", err)
	}

	if _, err := c.GetTask(t.Context(), "t1"); !errors.Is(err, errNotFound) || errors.As(err, &unavailable) {
		t.Errorf("get = %v, want errNotFound", err)
	}
	if _, err := c.UpdateTask(t.Context(), "t1", taskRequest{Name: "t"}); !errors.Is(err, errNotFound) {
		t.Errorf("update = %v, want errNotFound", err)
	}
	if err := c.DeleteTask(t.Context(), "t1"); !errors.Is(err, errNotFound) {
		t.Errorf("delete = %v, want errNotFound", err)
	}
}

func TestTaskClientReportsTheServersMessage(t *testing.T) {
	c := &taskClient{api: newTestSecretStoreClient(t,
		alwaysRespond(http.StatusConflict, `{"error":"a task named weekly-revenue already exists"}`))}

	_, err := c.CreateTask(t.Context(), taskRequest{Name: "weekly-revenue"})
	var apiErr *apiError
	if !errors.As(err, &apiErr) || apiErr.status != http.StatusConflict {
		t.Fatalf("got %v, want a 409 *apiError", err)
	}
	if !strings.Contains(err.Error(), "a task named weekly-revenue already exists") {
		t.Errorf("error %q does not carry the server's message", err)
	}
}

// The contract does not say which 2xx a create or a delete answers.
func TestTaskClientAcceptsAnySuccess(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusCreated} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := &taskClient{api: newTestSecretStoreClient(t, alwaysRespond(status, `{"id":"t1","name":"t"}`))}
			created, err := c.CreateTask(t.Context(), taskRequest{Name: "t"})
			if err != nil || created.ID != "t1" {
				t.Errorf("create = %+v, %v", created, err)
			}
		})
	}
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		t.Run("delete "+http.StatusText(status), func(t *testing.T) {
			c := &taskClient{api: newTestSecretStoreClient(t, alwaysRespond(status, ``))}
			if err := c.DeleteTask(t.Context(), "t1"); err != nil {
				t.Errorf("delete = %v", err)
			}
		})
	}
}

func TestTaskClientEscapesTheID(t *testing.T) {
	handler, last := record(t, http.StatusOK, `{"id":"t1"}`)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := &taskClient{api: &secretStoreClient{host: srv.URL, cred: auth.APIKey("test"), http: srv.Client()}}

	if got := c.taskURL("../users/u1?x=1"); got != srv.URL+"/api/v1/tasks/..%2Fusers%2Fu1%3Fx=1" {
		t.Errorf("taskURL = %q, want the id escaped into one path segment", got)
	}
	if _, err := c.GetTask(t.Context(), "a b"); err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if last.path != "/api/v1/tasks/a b" {
		t.Errorf("path = %q, want the id as one segment", last.path)
	}
}

// Every resource test goes through the fake, so it must refuse what the
// server refuses and answer in UTC as the server does.
func TestFakeTasksBehavesLikeTheServer(t *testing.T) {
	fake := newFakeTasks(t)
	c := fake.client()

	body := map[string]any{"name": "t", "prompt": "p", "trigger": map[string]any{"type": "once"}, "delivery": []any{}}
	payload, status, err := c.api.do(t.Context(), http.MethodPost, c.tasksURL(), body)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	if status != http.StatusBadRequest || !strings.Contains(string(payload), "delivery") {
		t.Errorf("POST with delivery = %d %s, want 400 naming the field", status, payload)
	}

	created, err := c.CreateTask(t.Context(), taskRequest{
		Name: "t", Prompt: "p",
		Trigger: taskTrigger{Type: "once", RunAt: "2026-12-01T09:00:00+01:00"},
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if created.Trigger.RunAt != "2026-12-01T08:00:00Z" {
		t.Errorf("run_at = %q, want it in UTC", created.Trigger.RunAt)
	}
}
