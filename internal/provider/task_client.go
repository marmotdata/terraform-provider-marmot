// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	marmot "github.com/marmotdata/marmot/sdk/go"
)

// taskClient calls the task endpoints, which the generated SDK does not
// cover. It shares the secret store client's transport and error shape.
type taskClient struct {
	api *secretStoreClient
}

func newTaskClient(c *marmot.Client) *taskClient {
	return &taskClient{api: newSecretStoreClient(c)}
}

// task is a task as the server returns it. next_run_at, last_run and the
// audit fields are left out: they change on their own and nothing here
// reads them.
type task struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Prompt      string        `json:"prompt"`
	Enabled     bool          `json:"enabled"`
	Trigger     taskTrigger   `json:"trigger"`
	RunAs       taskPrincipal `json:"run_as"`
	Sources     []string      `json:"sources"`
	Limits      taskLimits    `json:"limits"`
	CreatedAt   string        `json:"created_at"`
	UpdatedAt   string        `json:"updated_at"`
}

// taskTrigger is when a task runs. Only the fields of its type are sent.
type taskTrigger struct {
	Type            string      `json:"type"`
	RunAt           string      `json:"run_at,omitempty"`
	Cron            string      `json:"cron,omitempty"`
	Events          []string    `json:"events,omitempty"`
	Filter          *taskFilter `json:"filter,omitempty"`
	CooldownSeconds int64       `json:"cooldown_seconds,omitempty"`
}

// taskFilter narrows the events that fire an event trigger.
type taskFilter struct {
	AssetTypes []string `json:"asset_types,omitempty"`
	Providers  []string `json:"providers,omitempty"`
	Pipelines  []string `json:"pipelines,omitempty"`
}

// taskPrincipal is the principal a task runs as.
type taskPrincipal struct {
	Type string `json:"type"`
	ID   string `json:"id"`
	Name string `json:"name"`
}

// taskLimits caps one run. A zero or absent limit is the instance default,
// so an unset one is left out and the server fills it in.
type taskLimits struct {
	MaxSteps       int64 `json:"max_steps,omitempty"`
	MaxQueries     int64 `json:"max_queries,omitempty"`
	MaxRows        int64 `json:"max_rows,omitempty"`
	MaxTokens      int64 `json:"max_tokens,omitempty"`
	TimeoutSeconds int64 `json:"timeout_seconds,omitempty"`
}

// taskRequest is the body of a create and of an update. An update replaces
// the whole task, so Sources always goes, empty included.
type taskRequest struct {
	Name                  string      `json:"name"`
	Description           string      `json:"description"`
	Prompt                string      `json:"prompt"`
	Enabled               bool        `json:"enabled"`
	Trigger               taskTrigger `json:"trigger"`
	RunAsServiceAccountID string      `json:"run_as_service_account_id,omitempty"`
	Sources               []string    `json:"sources"`
	Limits                taskLimits  `json:"limits"`
}

// errNoTaskAPI means the instance cannot serve tasks. Marmot Cloud and
// Marmot Enterprise answer 501 on every task route while Tasks is not
// enabled. Open-source Marmot has no task routes at all, which only a
// request without an id in its path can tell: a 404 on POST /tasks is the
// route missing, while a 404 on /tasks/{id} may be the task gone.
type errNoTaskAPI struct {
	host   string
	status int
}

func (e *errNoTaskAPI) Error() string {
	return fmt.Sprintf("%s: no Tasks API (HTTP %d). Marmot Tasks is not enabled on this "+
		"instance, or not available to it: Tasks require Marmot Cloud or Marmot Enterprise "+
		"(https://cloud.marmotdata.io) with Tasks turned on. Check the provider's host, or "+
		"remove the marmot_task resources", e.host, e.status)
}

// call performs a request and decodes a 2xx response into out. A 501 is an
// *errNoTaskAPI, any other status an *apiError.
func (c *taskClient) call(ctx context.Context, method, endpoint string, body, out any) error {
	payload, status, err := c.api.do(ctx, method, endpoint, body)
	if err != nil {
		return err
	}
	if status == http.StatusNotImplemented {
		return &errNoTaskAPI{host: c.api.host, status: status}
	}
	if status < 200 || status > 299 {
		return newAPIError(status, payload)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("decoding response: %w", err)
	}
	return nil
}

func (c *taskClient) tasksURL() string {
	return c.api.host + "/api/v1/tasks"
}

// The id comes from state and import ids, so it is escaped.
func (c *taskClient) taskURL(id string) string {
	return c.tasksURL() + "/" + url.PathEscape(id)
}

// CreateTask creates a task. It runs as the caller unless the request names
// a service account.
func (c *taskClient) CreateTask(ctx context.Context, in taskRequest) (*task, error) {
	var out task
	err := c.call(ctx, http.MethodPost, c.tasksURL(), in, &out)
	var apiErr *apiError
	if errors.As(err, &apiErr) && apiErr.status == http.StatusNotFound {
		return nil, &errNoTaskAPI{host: c.api.host, status: apiErr.status}
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *taskClient) GetTask(ctx context.Context, id string) (*task, error) {
	var out task
	if err := c.call(ctx, http.MethodGet, c.taskURL(id), nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTask replaces a task. The server makes the task run as the caller
// again, or as the service account the request names.
func (c *taskClient) UpdateTask(ctx context.Context, id string, in taskRequest) (*task, error) {
	var out task
	if err := c.call(ctx, http.MethodPut, c.taskURL(id), in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *taskClient) DeleteTask(ctx context.Context, id string) error {
	return c.call(ctx, http.MethodDelete, c.taskURL(id), nil, nil)
}
