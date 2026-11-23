---
page_title: "Tasks"
subcategory: "Guides"
description: |-
  Declare a prompt an agent completes in the background, decide whose access it runs with, and choose when it runs.
---

# Tasks

~> **Requires Marmot Cloud or Marmot Enterprise.** Open-source Marmot has no Tasks API, and an instance where Tasks is not enabled refuses every request, so `marmot_task` fails on apply there.

A task is a goal written as a prompt. Marmot runs an agent in the background to complete it whenever the task's trigger fires. Each run keeps its status, a step by step record of what the agent did, and the result, for people with the right role to read in Marmot.

The agent can search the catalog, read an asset, list the queryable sources and their tables, and run a governed, read-only query. It has no tool that writes anything or sends anything out.

```terraform
resource "marmot_task" "weekly_revenue" {
  name   = "weekly-revenue"
  prompt = "Report last week's revenue by region and compare it with the week before."

  trigger = {
    type = "schedule"
    cron = "0 8 * * 1"
  }
}
```

## Who a task runs as

A task has no access of its own. It runs as a principal, and the catalog and every query make the same decisions for the task that they make for that principal anywhere else.

By default that principal is whoever last saved the task. Under Terraform that is the principal behind the provider's credential: you, when you apply from a `marmot login` session, or the CI service account.

| `run_as_service_account_id` | The task runs as |
| --- | --- |
| Not set | The principal that applied the last change. |
| Set | That service account. Whoever applies needs `serviceAccount:manage` on the organization. |

Every change re-owns the task. Nobody can edit a prompt and have it run with someone else's access, so each apply that changes a task makes it run as the applying principal again, even when the task was last saved by someone else in the UI. The computed `run_as` reports who that is, and shows as known after apply on every update for that reason.

Name a service account. A task that follows whoever ran `terraform apply` last changes its access when a colleague applies, and the CI account that manages the configuration usually holds grants a report has no use for.

```terraform
resource "marmot_service_account" "revenue_reporter" {
  name        = "revenue-reporter"
  description = "Runs the weekly revenue task. Owned by the finance team."
}

resource "marmot_asset_iam_member" "reporter_queries_revenue" {
  asset_id = marmot_asset.revenue.id
  role     = "asset.dataQuerier"
  member   = "serviceAccount:${marmot_service_account.revenue_reporter.id}"
}

resource "marmot_task" "weekly_revenue" {
  name   = "weekly-revenue"
  prompt = "Report last week's revenue by region and compare it with the week before."

  trigger = {
    type = "schedule"
    cron = "0 8 * * 1"
  }

  run_as_service_account_id = marmot_service_account.revenue_reporter.id
  sources                   = ["warehouse"]
}
```

Grant the account what the task needs to read and query, and nothing more (see the [access control guide](access-control.md) for the roles). `sources` narrows the task further, to some of the sources the account may query. It never widens anything.

Marmot loads the principal again at the start of every run. If the account was deleted or deactivated, the run fails and the task is paused.

## When a task runs

| `trigger.type` | Runs | Takes |
| --- | --- | --- |
| `once` | One time: at `run_at`, or when the task is created. | `run_at` (optional) |
| `schedule` | On a cron, in UTC. | `cron` (five fields) |
| `event` | When the catalog changes. | `events`, and optionally `filter` and `cooldown_seconds` |

Events are debounced and folded into at most one pending run per task, so a burst of schema changes produces one run, not one per change.

```terraform
resource "marmot_task" "schema_watch" {
  name   = "schema-watch"
  prompt = "Describe what changed in the schema and which downstream assets it may break."

  trigger = {
    type   = "event"
    events = ["asset.schema_changed"]

    filter = {
      asset_types = ["table"]
    }

    cooldown_seconds = 900
  }
}
```

A `once` trigger without `run_at` runs when the resource is created and never again. Editing the prompt later updates the task without running it. To run it again, replace it:

```shell
terraform apply -replace=marmot_task.orphaned_tables
```

Set `enabled = false` to keep a task and stop it from running.

## Limits

Each run is capped on steps, queries, rows per query, model tokens and wall-clock time. Leave `limits` out and the server fills in the instance defaults, which then show in state. Set only the ones you want to differ:

```terraform
resource "marmot_task" "weekly_revenue" {
  # ...

  limits = {
    max_queries     = 5
    timeout_seconds = 300
  }
}
```

The limits you do not set show as known after apply whenever the task changes, because the server decides them again. Removing a limit from the configuration is not a change by itself: the limit keeps its value until the task next changes, and then goes back to the instance default.

## Who can manage tasks

Task permissions are granted on the organization, not per task.

| Role | Can |
| --- | --- |
| `task.viewer` | See task definitions, run status and usage. |
| `task.reader` | Read results and the step record of a run, which can contain data. |
| `task.editor` | Create, update, delete, run and cancel tasks. |

```terraform
resource "marmot_organization_iam_member" "terraform_manages_tasks" {
  role   = "task.editor"
  member = "serviceAccount:${marmot_service_account.terraform.id}"
}
```

The principal that applies `marmot_task` needs `task.editor`. Results can contain query output, so give `task.reader` to the people who should see that data and `task.viewer` to everyone else.

## Importing a task

```shell
terraform import marmot_task.weekly_revenue 018e1234-5678-7abc-def0-123456789abc
```

The server reports who a task runs as, not whether a request named that service account. After an import `run_as_service_account_id` is absent from state, so a configuration that sets it shows it as an addition in the first plan. Applying that sends the whole task again, which makes it run as the account it names.

## Troubleshooting

| Symptom | Usually |
| --- | --- |
| `Marmot Tasks Is Not Enabled or Not Available` | The instance answered 501: Tasks is not enabled on it. On open-source Marmot the same error appears on create, since the API is not there. |
| `run_as` changed after an apply | Expected. Every change makes the task run as the principal that applied it. Set `run_as_service_account_id` to pin it. |
| A `once` task did not run after an edit | Expected. It runs when created. Replace it with `-replace` to run it again. |
| The plan shows `run_as_service_account_id` as added after an import | The server never returns it. Apply once to send it. |
| Runs fail and the task is paused | The principal in `run_as` was deleted or deactivated. Point `run_as_service_account_id` at an active account. |
