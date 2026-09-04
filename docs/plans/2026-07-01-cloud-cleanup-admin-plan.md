# Bitbucket Cloud Cleanup and Access Plan

## Problem

`bkt` already covers day-to-day Bitbucket Cloud developer workflows, but it lacks first-class commands for Cloud administrative cleanup and access management.

The main gaps for Bitbucket Cloud are:
- No `repo delete` command
- No Cloud `project` commands
- No Cloud `perms` commands
- No bulk cleanup workflow for stale repositories or empty projects
- No desired-state access sync for applying a standard user/group model across many projects or repositories

The raw `bkt api` command can reach the required endpoints today, but the user experience is too low-level for common admin tasks.

## Goals

- Add safe first-class cleanup commands for Bitbucket Cloud
- Make project cleanup practical, including deleting a project after its repos are removed
- Add first-class Cloud permission management for users and groups
- Add a repeatable way to apply a standard access model across many projects or repos
- Preserve `bkt` conventions: structured output, interactive confirmation for destructive actions, and scriptable non-interactive flags

## Non-Goals

- Full Bitbucket Cloud organization governance in the first pass
- Automatic deletion based only on heuristics without a review step
- Hiding Bitbucket Cloud auth limitations for permission-changing endpoints

## Product Direction

Use three layers:

1. Core Cloud admin primitives
2. Cleanup discovery commands built on those primitives
3. Desired-state access sync for repeatable policy application

This keeps the first milestone small while making the higher-level workflows possible.

## Proposed Command Roadmap

### Phase 1: Core destructive primitives

Add the minimum commands needed to manage Cloud repos and projects safely.

#### `bkt repo delete`

Delete a Bitbucket Cloud repository.

Proposed flags:
- `--workspace`
- `--repo`
- `--yes`

Behavior:
- Cloud only in the first pass
- Fetch repo details and the last commit before deletion so the confirmation prompt can show the repo name and the last commit message, date and branch.
- Prompt by default, skip with `--yes`
- Return structured JSON/YAML output with `deleted: true`

Example:

```bash
bkt repo delete old-service
bkt repo delete --workspace myteam --repo old-service --yes
```

#### Expand `bkt project` to Bitbucket Cloud

Add Cloud project commands instead of keeping `project` as DC-only.

Recommended subcommands:
- `bkt project list`
- `bkt project view <project-key>`
- `bkt project repos <project-key>`
- `bkt project delete <project-key>`
- `bkt project rename <project-key> --name <new-name>`

Cloud delete flags:
- `--workspace`
- `--cascade`
- `--yes`

Behavior:
- `project delete` without `--cascade` should fail with a clear error if the project still contains repositories
- `project delete --cascade` should list candidate repos, confirm once, delete repos, then delete the project
- `project repos` should make project-scoped cleanup easy without requiring `bkt api`

Examples:

```bash
bkt project list --workspace myteam
bkt project repos WEB --workspace myteam
bkt project delete WEB --workspace myteam
bkt project delete WEB --workspace myteam --cascade --yes
bkt project rename DFW --workspace myteam --name "ZZ - Archived - DFW"
```

#### `bkt project rename`

Rename a Bitbucket Cloud project. The project key is not changed; only the
display name (and optionally description) are updated.

Flags:
- `--workspace`
- `--name` (required)
- `--description`

Use cases:
- General project renaming without changing project keys or repository URLs.
- Manual archive-style sorting.

#### `bkt project archive`

Rename a project by prepending a configurable archive prefix. The current project
name is looked up automatically, so the prefix is added without the user having
to retype the existing name.

Flags:
- `--workspace`
- `--prefix` (default: `"ZZ - Archived - "`)
- `--description`

Behavior:
- Fetches the current project name.
- Computes the new name as `<prefix><current name>`.
- If the current name is empty, falls back to `<prefix><project key>`.
- Calls the project rename endpoint.
- Returns structured output with `old_name`, `new_name`, and `prefix`.

Example:

```bash
# Archive using the default "ZZ - Archived - " prefix
bkt project archive DFW --workspace my-team

# Result: project name becomes "ZZ - Archived - DFW Airport"
```

### Phase 2: Cleanup discovery

Add read-only commands that help identify deletion candidates before any destructive action.

#### Recommended command shape

Prefer a new `cleanup` group instead of overloading `repo list` and `project list` with too many audit flags.

Recommended subcommands:
- `bkt cleanup repos`
- `bkt cleanup projects`

#### `bkt cleanup repos`

List repository cleanup candidates for a workspace or project. This command is
read-only: it never deletes repositories. Use the output to drive explicit
`bkt repo delete` or `bkt project delete --cascade` calls.

Required flag:
- `--workspace <workspace>`

Optional scope flag:
- `--project <project-key>` — restrict to a single project

Heuristic flags (at least one required; OR logic):
- `--inactive-for <n>d` — `updated_on` older than `n` days (default: `180d`)
- `--empty` — repository has no commits
- `--public` — repository is public
- `--private` — repository is private

Exclusion flag:
- `--exclude-pattern <glob>` — case-insensitive glob against the repository slug

Output control:
- `--limit <n>` — maximum repositories to evaluate (0 for all)

Recommended output fields:
- `workspace`
- `project_key`
- `repo`
- `visibility`
- `created_on`
- `updated_on`
- `default_branch`
- `reason[]`
- `delete_candidate`

Inactivity signal:
- Uses the repository metadata `updated_on` field.
- Atlassian documents `updated_on` as reflecting the last commit activity,
  including branch creation/deletion, pull request source branch updates, and
  commit history rewrites. It does not include pull request metadata edits,
  pipeline build updates, or pipeline repository clone/read events.
- The first implementation does not fetch the commits endpoint per repo.

Removed from the first pass:
- `--include-archived` — Bitbucket Cloud has no native repository archive state
- `last_commit_on` — using `updated_on` instead
- `open_pr_count` — requires extra pull request API calls

Example:

```bash
bkt cleanup repos --workspace myteam --inactive-for 180d --json
bkt cleanup repos --workspace myteam --project WEB --empty
bkt cleanup repos --workspace myteam --inactive-for 365d --exclude-pattern '*-legacy'
```

#### `bkt cleanup projects`

List project cleanup candidates for a workspace. This command is read-only.

Required flag:
- `--workspace <workspace>`

Heuristic flags (at least one required; OR logic):
- `--inactive-for <n>d` — every repository in the project has `updated_on` older than `n` days (default: `180d`)
- `--empty` — project has no repositories

Output control:
- `--limit <n>` — maximum projects to evaluate (0 for all)

Output fields:
- `workspace`
- `project_key`
- `name`
- `repo_count`
- `empty`
- `all_repos_inactive`
- `latest_repo_updated_on`
- `reason[]`
- `archive_candidate`
- `delete_candidate`

A project is a `delete_candidate` when it is empty (and `--empty` is used) or when
every repo is inactive (and `--inactive-for` is used). All matching projects are
`archive_candidate`.

Inactivity signal:
- Uses the repository metadata `updated_on` field.
- A project only matches `--inactive-for` when **every** repository in the project
  is inactive. If any repo is active, the project is not a candidate.

Example:

```bash
bkt cleanup projects --workspace myteam --empty
bkt cleanup projects --workspace myteam --inactive-for 365d --json
```

### Archive strategy for Bitbucket Cloud

Bitbucket Cloud has no native repository or project archive feature. The chosen
archive strategy is **Option B: rename projects to sort them out of the active
project list**.

Recommended workflow:

1. Use `bkt cleanup projects` to identify empty or fully inactive projects.
2. For each candidate, archive it with the default prefix:
   ```bash
   bkt project archive DFW
   ```
   This is equivalent to `bkt project rename DFW --name "ZZ - Archived - <current name>"`,
   but the current name is fetched automatically.
3. Optionally update the project description to note the archive date:
   ```bash
   bkt project archive DFW --description "Archived on 2026-07-01"
   ```
4. Leave the original project key and repositories in place; no repository URLs
   change.

This does not reduce the total number of projects in the workspace, but it keeps
active projects at the top of alphabetically sorted lists while preserving full
repository history, pull requests, and access.

Alternative not chosen:
- **Option A**: move all archived repos into a single `ARCHIVES` project and delete
  the original projects. This truly reduces project count but requires renaming
  and moving every repository, which changes repository URLs.

### Phase 3: Access management and desired-state sync

Extend `bkt perms` to support Bitbucket Cloud, then add a sync command for repeatable policy application.

#### Expand `bkt perms` for Cloud

Recommended additions:
- `bkt perms project list`
- `bkt perms project grant`
- `bkt perms project revoke`
- `bkt perms repo list`
- `bkt perms repo grant`
- `bkt perms repo revoke`

Cloud-specific flags:
- `--workspace`
- `--project`
- `--repo`
- `--user`
- `--group`
- `--perm read|write|admin|create-repo`

Notes:
- Cloud permissions need both group and user support
- The CLI should reject invalid flag combinations like passing both `--user` and `--group`
- Permission mutation commands must document auth restrictions clearly because some Bitbucket Cloud endpoints require app passwords or scoped API tokens rather than OAuth/JWT

#### Add `bkt perms sync`

Apply a standard access model from a YAML file to many targets.

Recommended flags:
- `--workspace`
- `--file`
- `--project`
- `--repo`
- `--selector`
- `--dry-run`
- `--yes`

Example config:

```yaml
workspace: myteam
defaults:
  project_permissions:
    groups:
      engineering-leads: admin
      engineers: write
    users:
      contractor@example.com: read
  repo_permissions:
    groups:
      sre: admin
targets:
  projects:
    - WEB
    - DATA
  repos:
    - web-frontend
    - data-pipeline
```

Behavior:
- Read current permissions
- Compute the diff against desired state
- Show adds, updates, and removals
- Require confirmation unless `--yes` is passed
- Support `--dry-run` for review-only execution

This is the command that makes "apply a consistent set of permissions on a set of projects or repos" practical.

## Stale / Inactive Heuristic

"No recent commits" is a good signal, but it should not be the only deletion signal.

Recommended model:

### Repository inactivity

Classify a repo as inactive when:
- The latest commit on the default branch is older than the threshold
- The repo is older than the threshold grace period
- There are no open PRs

Useful additional signals:
- Repository metadata `updated_on` older than threshold
- No pipelines run recently if pipeline data is available later
- No default reviewers or special access rules may increase confidence, but should not drive deletion alone

Important distinction:
- `updated_on` is a fast broad filter based on commit activity
- `last_commit_on` would be a more precise decision signal but requires one
  extra API call per repository

The first implementation uses `updated_on` only. Future iterations can add a
`--with-last-commit` flag to enrich candidates with the latest default-branch
commit timestamp.

### Project inactivity

Classify a project as inactive when:
- It is empty, or
- Every repo in the project is inactive, and
- The most recent repo commit in the project is older than the threshold

Projects should not be auto-deleted based solely on age. They should be surfaced as candidates first.

## Recommended API / Data Changes

To support cleanup commands well, expand the Cloud repository model to include fields that the API already returns and the CLI does not currently expose.

Recommended additions to `pkg/bbcloud.Repository`:
- `Description`
- `CreatedOn`
- `UpdatedOn`

Recommended new Cloud client helpers:
- `ListProjects(workspace, limit)`
- `GetProject(workspace, projectKey)`
- `CreateProject(workspace, input)`
- `UpdateProject(workspace, projectKey, input)`
- `DeleteProject(workspace, projectKey)`
- `DeleteRepository(workspace, repoSlug)`
- `ListProjectGroupPermissions(workspace, projectKey)`
- `SetProjectGroupPermission(...)`
- `DeleteProjectGroupPermission(...)`
- `ListProjectUserPermissions(workspace, projectKey)`
- `SetProjectUserPermission(...)`
- `DeleteProjectUserPermission(...)`
- `ListRepoGroupPermissions(workspace, repoSlug)`
- `SetRepoGroupPermission(...)`
- `DeleteRepoGroupPermission(...)`
- `ListRepoUserPermissions(workspace, repoSlug)`
- `SetRepoUserPermission(...)`
- `DeleteRepoUserPermission(...)`

For stale-repo enrichment in future iterations:
- `GetDefaultBranchTip(workspace, repoSlug)` or
- `ListCommits(workspace, repoSlug, include=<default-branch>, limit=1)`

## Safety Model

Destructive cleanup commands should follow the existing patterns used elsewhere in the repo.

Recommended rules:
- Prompt by default for delete operations
- Support `--yes` for automation
- Support `--dry-run` where the command computes impact without mutating
- Print exactly what will be deleted before confirmation for multi-repo cascades
- Return machine-readable structured output in all cases

For bulk operations, avoid deleting while discovering. Prefer:

1. `bkt cleanup ... --json`
2. Review results
3. Target the specific repo/project delete command

## Suggested Delivery Order

1. Add Cloud `repo delete` — done
2. Add Cloud `project list/view/repos/delete/rename` — done
3. Expand Cloud repository model with `updated_on`, `created_on`, and main branch data — done
4. Add `bkt cleanup repos` — done
5. Add `bkt cleanup projects` — done
6. Extend `bkt perms` for Cloud list/grant/revoke
7. Add `bkt perms sync`

This order gets useful cleanup workflows into users' hands quickly and leaves the more complex desired-state permission syncing for after the basic primitives are proven.

## Test Plan

- Unit tests for Cloud repo deletion confirmation, `--yes`, and output
- Unit tests for Cloud project delete with and without `--cascade`
- Unit tests for project delete short-circuit when repos remain and `--cascade` is not set
- Unit tests for Cloud project rename, including name and description updates
- Unit tests for `bkt cleanup repos` candidate classification and glob exclusions
- Unit tests for `bkt cleanup projects` empty and fully-inactive detection
- Unit tests for permission diffing in `perms sync`
- Tests that invalid Cloud auth modes produce actionable permission-mutation errors

## Open Questions

- Should `bkt cleanup repos` support deletion directly, or stay read-only and force an explicit `repo delete` step?
- Should `project delete --cascade` delete every repo unconditionally, or require a second confirmation listing each repo?
- Should `perms sync` support removals by default, or only with an explicit `--prune` flag?
- Do we want one generic `cleanup` group for both Cloud and DC, or Cloud-only cleanup first?
- Should we later add a bulk `project archive` helper that automates the rename-based archive workflow?

## Recommendation

Start with Cloud cleanup primitives and keep them conservative:

- `repo delete`
- `project list/view/repos/delete/rename --cascade`
- `project archive`
- `cleanup repos`
- `cleanup projects`

Then add Cloud permissions in two steps:

- singular list/grant/revoke commands under `perms`
- a higher-level `perms sync` command driven by YAML

That gives you practical repo/project cleanup quickly, while setting up a clean path to bulk access standardization.
