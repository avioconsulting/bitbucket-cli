# Backlog

## Permission Audits And Reconciliation

- Add `bkt perms audit repo` to inventory direct repository user and group grants, grouped by project. Support `--project`, `--group`, `--user`, `--archived`, and structured output.
- Add `bkt perms repo clear --project <key> --all` with a preview by default and `--apply` required for mutations. It should revoke every direct repository user and group grant, report counts, and verify an empty result.
- Add `bkt perms project apply-policy` for the standard policy, AWS, AVIOMSP, MBC, and archived-project overrides. It must preview drift, apply only required changes, and verify final state.
- Add `bkt project archive --clear-repo-perms` as an explicit opt-in. It should show the affected repository grants before changing them.
- Accept opaque legacy Atlassian account IDs returned by permission APIs when revoking direct user access. Add an explicit `--account-id` escape hatch and regression coverage.

## Group Commands

- Add `httptest` coverage for every `bkt group` and `bkt invite` legacy API route, including methods, bodies, URL escaping, Cloud-only validation, identity resolution, and API errors.
- Document `bkt group delete` safety expectations and legacy Bitbucket group limitations.

## Scheduling

- Add a documented systemd timer and CI schedule example for read-only permission, archived-project, group-membership, and inactivity audits.
- Keep scheduled jobs report-only; never schedule permission mutations.
