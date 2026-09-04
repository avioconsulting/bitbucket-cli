# Bitbucket Cloud Project Commands Plan

## Goal

Add first-class Bitbucket Cloud support for these four `project` subcommands:

- `bkt project list`
- `bkt project view <project-key>`
- `bkt project repos <project-key>`
- `bkt project delete <project-key>`

This milestone should make Cloud projects practical to inspect and clean up without dropping down to `bkt api`.

## Scope

In scope:
- Cloud project listing
- Cloud project detail view
- Listing repositories assigned to a Cloud project
- Deleting an empty Cloud project
- Structured JSON/YAML output for every command
- Command help and generated docs
- Unit tests for Cloud client and command behavior

Out of scope for this milestone:
- `project delete --cascade`
- Bulk cleanup heuristics
- Permission management
- URL parsing or repo-to-context inference changes
- Full refactor of existing Data Center project command structure

## Product Decisions

### 1. Keep `project` as one command group for both DC and Cloud

Do not create a new Cloud-only top-level command.

Instead:
- keep `bkt project list` working for Data Center
- extend `bkt project` so subcommands branch by host kind where needed
- update command help to describe both platforms accurately

### 2. Use `project repos` instead of adding project filters to `repo list`

`project repos` is the right shape for cleanup workflows and matches the user’s mental model.

### 3. Keep first-pass delete conservative

`bkt project delete` should:
- delete only empty Cloud projects in this milestone
- refuse to delete non-empty projects with an actionable error
- defer `--cascade` to the next milestone

That keeps the initial implementation smaller and safer.

## Expected CLI Surface

### `bkt project list`

Cloud behavior:
- list projects in the active workspace or `--workspace`
- support `--limit`

Flags:
- keep `--host` for DC behavior
- add `--workspace` for Cloud behavior
- keep `--limit`

Examples:

```bash
bkt project list --workspace myteam
bkt project list --limit 100 --json
```

### `bkt project view <project-key>`

Cloud behavior:
- show a project’s key, name, description, privacy, timestamps, and web URL

Flags:
- `--workspace`

Examples:

```bash
bkt project view WEB --workspace myteam
bkt project view WEB --json
```

### `bkt project repos <project-key>`

Cloud behavior:
- list repos in the workspace whose `project.key` matches the target key
- support `--limit`

Flags:
- `--workspace`
- `--limit`

Examples:

```bash
bkt project repos WEB --workspace myteam
bkt project repos WEB --limit 0 --json
```

### `bkt project delete <project-key>`

Cloud behavior:
- fetch the project first for confirmation context
- check whether the project still has repositories
- if repo count > 0, fail with a clear error and do not delete
- if repo count == 0, prompt by default and delete on confirmation
- support `--yes`

Flags:
- `--workspace`
- `--yes`

Examples:

```bash
bkt project delete WEB --workspace myteam
bkt project delete WEB --workspace myteam --yes
```

## API Plan

### Cloud project endpoints

Add first-class Cloud client support in a new file, preferably:

- `pkg/bbcloud/projects.go`

Recommended types:

```go
type Project struct {
    UUID                     string `json:"uuid"`
    Key                      string `json:"key"`
    Name                     string `json:"name"`
    Description              string `json:"description"`
    IsPrivate                bool   `json:"is_private"`
    CreatedOn                string `json:"created_on"`
    UpdatedOn                string `json:"updated_on"`
    HasPubliclyVisibleRepos  bool   `json:"has_publicly_visible_repos"`
    Links struct {
        HTML struct {
            Href string `json:"href"`
        } `json:"html"`
    } `json:"links"`
}
```

Recommended methods:

- `ListProjects(ctx, workspace string, limit int) ([]Project, error)`
- `GetProject(ctx, workspace, projectKey string) (*Project, error)`
- `DeleteProject(ctx, workspace, projectKey string) error`

Endpoints:
- `GET /workspaces/{workspace}/projects`
- `GET /workspaces/{workspace}/projects/{project_key}`
- `DELETE /workspaces/{workspace}/projects/{project_key}`

### Project repos strategy

Bitbucket Cloud does not appear to expose a simple dedicated project-repos endpoint that fits this CLI use case.

First-pass implementation should:
- reuse existing `ListRepositories(ctx, workspace, limit)`
- filter repos in memory by `repo.Project.Key`

This is acceptable because:
- `Repository.Project.Key` is already present in the repo model
- it avoids premature API abstraction work
- it keeps `project repos` easy to ship now

If we later need server-side filtering or very large workspace optimization, we can revisit it.

## Command Implementation Plan

### 1. Extend top-level `project` command metadata

File:
- `pkg/cmd/project/project.go`

Update:
- top-level `Short` and `Long` strings so they no longer claim DC-only support
- examples should include both DC and Cloud usage where helpful

### 2. Refactor `project list` to branch on host kind

Current code is DC-only and resolves hosts directly.

Recommended changes:
- update `listOptions` to include `Workspace`
- preserve `Host` for DC use
- in `runList`, branch by `hostCfg.Kind`

DC path:
- keep current behavior intact

Cloud path:
- resolve workspace from `--workspace` or active context
- build Cloud client
- call `ListProjects`
- render Cloud-specific project summaries

### 3. Add `project view`

Implement as a new subcommand in `pkg/cmd/project/project.go`.

Recommended signature:
- `Use: "view <project-key>"`

Behavior:
- for Cloud: fetch by key and render details
- for DC: return a clear not-yet-supported error in this milestone, or omit the DC path entirely with explicit docs

Recommendation:
- implement Cloud only
- reject DC with a direct, explicit message

That keeps the behavior honest and avoids silently promising parity we do not yet have.

### 4. Add `project repos`

Implement as a new subcommand in `pkg/cmd/project/project.go`.

Recommended signature:
- `Use: "repos <project-key>"`

Behavior:
- Cloud only for this milestone
- resolve workspace
- fetch repositories from workspace
- filter by uppercase normalized `project.key`
- render a repo list similar to `repo list`

Output fields should include:
- `workspace`
- `project`
- `repositories[]`
  - `slug`
  - `name`
  - `uuid`
  - `web_url`
  - `clone_urls`

### 5. Add `project delete`

Implement as a new subcommand in `pkg/cmd/project/project.go`.

Recommended signature:
- `Use: "delete <project-key>"`
- alias `rm`

Behavior:
- Cloud only for this milestone
- resolve workspace
- fetch project for confirmation context
- fetch project repos using the same helper logic as `project repos`
- if repos exist:
  - do not delete
  - return an error like:
    - `project WEB still contains 3 repositories; delete them first`
- if empty:
  - prompt unless `--yes`
  - delete project
  - emit structured output with `deleted: true`

Recommended output:

```json
{
  "workspace": "myteam",
  "project": "WEB",
  "name": "Web Platform",
  "deleted": true
}
```

## Shared Helpers

To avoid repeated logic inside `pkg/cmd/project/project.go`, add small local helpers such as:

- `resolveCloudWorkspace(...)`
- `normalizeProjectKey(...)`
- `filterRepositoriesByProjectKey(...)`

Keep them in the same file unless they clearly need reuse elsewhere.

## Test Plan

### Cloud client tests

Add a new test file:
- `pkg/bbcloud/projects_test.go`

Cover:
- `ListProjects`
- `GetProject`
- `DeleteProject`
- validation for empty workspace or empty key
- pagination trimming for list behavior

### Command tests

Extend or add tests under:
- `pkg/cmd/project/project_test.go`

Cover:

`project list`
- DC path still works
- Cloud path lists workspace projects
- missing workspace in Cloud errors clearly

`project view`
- Cloud path returns details
- missing key errors from Cobra arg validation
- DC path rejects clearly if left unsupported

`project repos`
- filters workspace repos by `project.key`
- empty project shows no repos cleanly
- limit handling if implemented by fetch-then-trim

`project delete`
- prompts by default
- `--yes` skips prompt
- non-empty project refuses deletion
- empty project deletes successfully
- decline path prints `Aborted.`

### Docs generation verification

Regenerate:
- `skills/bkt/rules/project.md`

Verify the generated docs reflect:
- Cloud support in the top-level description
- the four subcommands
- `--workspace` and `--yes` where appropriate

## Build and Verification Steps

Minimum verification before merge:

```bash
gofmt -w pkg/bbcloud/projects.go pkg/bbcloud/projects_test.go pkg/cmd/project/project.go pkg/cmd/project/project_test.go
go test ./pkg/bbcloud ./pkg/cmd/project
go run ./cmd/docgen -o skills/bkt/rules
go test ./...
go build ./cmd/bkt
```

## Recommended Delivery Order

1. Add `pkg/bbcloud/projects.go` and tests
2. Extend `project list` for Cloud
3. Add `project view`
4. Add `project repos`
5. Add `project delete`
6. Regenerate docs
7. Run full tests and build

## Follow-up After This Milestone

Once the four project commands are in place, the next logical step is:

- `bkt project delete --cascade`

That can reuse the new project and project-repo plumbing rather than inventing a separate cleanup path.
