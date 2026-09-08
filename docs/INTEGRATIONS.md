# Adopting vlotpipe gradually

Five ways to wire vlotpipe in, roughly ordered from lowest-friction to
most enforced. None of them require the others — pick where a team
actually is today, not where the ideal end state is. A common path:
start with CI annotations only (visibility, no gate), add the
pre-commit hook once people are used to seeing findings, only then
switch `check` from advisory to a required status check. Once `check`
is a real gate, [`select`](SELECTIVE_ENFORCEMENT.md) is what makes that
transition gradual too — gate on a small, currently-clean set of rules
first, rather than the whole rule set at once.

## 1. CI annotations (visibility, zero extra setup)

Every `scan`/`check` run already knows the location of every finding —
`--format github` and `--format azure-devops` just express that as the
platform's own native annotation syntax instead of plain text, so
findings show up as inline comments directly on the "Files changed" /
"Files" tab of a pull request. No bot, no app installation, nothing to
grant permissions to:

```yaml
# GitHub Actions
- run: vlotpipe check . --format github
```

```yaml
# Azure Pipelines
- script: vlotpipe check . --format azure-devops
```

**Even better: leave `--format` off entirely.** Both `scan` and `check`
detect the CI environment automatically (`GITHUB_ACTIONS`/`TF_BUILD`,
the variables each platform sets on every run) and switch to the native
annotation format on their own — an explicit `--format` always wins,
this only fills in when the flag is omitted. A step that already runs
`vlotpipe check .` starts producing inline annotations with no change
at all.

Azure's logging commands only define two issue types (error/warning,
no "notice") — `info`-severity findings map to `warning`, the less
severe of the two, rather than being silently dropped.

## 2. A composite GitHub Action

[`action.yml`](../action.yml) at the repo root wraps install + run +
annotate into one step:

```yaml
- uses: vlotra/vlotpipe@v1        # pin to a tag once this repo has releases
  with:
    fail-on: blocker              # blocker | warning | info | never
```

`fail-on` is deliberately a separate concept from `severity` (what gets
*reported*): `severity: info` shows everything but `fail-on: blocker`
only fails the build on a blocker, which is the useful combination for
"tell me everything, but don't block the merge on the small stuff" —
also why `fail-on: never` exists, for rolling this out without gating
anything on day one. Under the hood this runs a second, silent JSON
pass and counts findings at or above the threshold itself, since
`vlotpipe check`'s own exit code only distinguishes "any blocker" from
"none" — not granular enough for `fail-on: warning`/`info` on its own.

## 3. `pre-commit` framework hook

If the repo already uses [pre-commit](https://pre-commit.com) for other
tools (black, ruff, prettier, ...), this is a two-line addition to
`.pre-commit-config.yaml`:

```yaml
repos:
  - repo: https://github.com/vlotra/vlotpipe
    rev: v0.1.0  # pin to a tag once this repo has releases
    hooks:
      - id: vlotpipe          # check only
      # - id: vlotpipe-fix     # check, but auto-apply --fix first
      # - id: vlotpipe-format  # format --check — surgical indent-only by default, see below
```

Defined in [`.pre-commit-hooks.yaml`](../.pre-commit-hooks.yaml). Only
lints the files actually staged in that commit (pre-commit passes them
as arguments — `vlotpipe scan/check` already accepts an explicit file
list, nothing hook-specific was needed on the CLI side), so this stays
fast regardless of repo size. `vlotpipe-fix` is the closest thing to
"just make it pass" — it applies `--fix`'s safe, mechanical edits before
checking, so most of what it would have blocked on never reaches the
commit at all.

## 4. A raw git hook (no framework dependency)

For a repo that doesn't want a `pre-commit` framework dependency just
for this, [`scripts/pre-commit`](../scripts/pre-commit) is a plain bash
script doing the same thing — stage-aware, fast, and self-contained:

```
cp scripts/pre-commit .git/hooks/pre-commit
chmod +x .git/hooks/pre-commit
```

It warns and lets the commit through if `vlotpipe` isn't on `PATH`,
rather than hard-blocking a teammate who hasn't installed it yet — the
comment in the script marks exactly where to change that to a hard
`exit 1` once a team has standardized on having it installed.

## 5. Pushing to a dashboard

Every other option above is local: a finding gets shown or gates a
build in the one repo/run it was found in. `report.to` in
`.vlotpipe.yml` (or `--report-to`) is different — it POSTs the
**complete, unfiltered** finding set to a configurable endpoint after
every scan, for a fleet-wide dashboard aggregating across a team's
repos and branches. "Complete" is deliberate: the push is never
narrowed by [`select` or `report.select`](SELECTIVE_ENFORCEMENT.md) —
those control what gates a build or prints locally, but the dashboard's
entire value is seeing everything, even from a repo whose local gate is
scoped down to three rules.

```yaml
# .vlotpipe.yml
report:
  to: "https://dashboard.example.com/api/ingest"
```

Resolved in this order, first non-empty wins — the URL is fine to
commit, so this exists mainly for environments that want to override it
per-invocation without editing the repo:

1. `--report-to <url>`
2. `VLOTPIPE_REPORT_TO` (env var)
3. `report.to:` in `.vlotpipe.yml`

**The auth token has no config-file option at all.** `.vlotpipe.yml` is
a committed file, and a bearer token hardcoded into it is exactly the
`SEC002` pattern vlotpipe itself flags in pipeline YAML — the tool
doesn't reproduce that mistake in its own config. Set
`VLOTPIPE_REPORT_TOKEN` (env var only); if set, it's sent as
`Authorization: Bearer <token>`. If unset, no `Authorization` header is
sent at all — some endpoints on a private network may not need one.

A failed push (unreachable host, non-2xx response) is printed as a
`warning:` on stderr and never fails the scan or changes `check`'s exit
code — an optional, separately-operated dashboard being briefly down
must never break the actual lint gate.

The payload:

```json
{
  "repo": "origin's remote URL, best-effort from git — empty if not a git repo",
  "branch": "current branch, best-effort from git",
  "commit": "current commit, best-effort from git",
  "scanned_at": "RFC 3339 timestamp",
  "files_scanned": 12,
  "violations": [ /* every finding — full Violation objects, same shape as --format json */ ],
  "fingerprints": [ /* one entry per job with enough steps to fingerprint, see below */ ]
}
```

`fingerprints` is the input to cross-repo duplicate-job detection (see
[`docs/adr/0004-duplicate-job-fingerprinting.md`](adr/0004-duplicate-job-fingerprinting.md)):
a structural signature (`internal/fingerprint`, simhash-based) plus a
file/job pointer, deliberately never the job's actual step content — a
job's `uses`/`run` text never leaves the scanning machine, only enough to
let a central service say "this job matches one found elsewhere" and
point at both locations. `vlotpipe scan`/`check` already print every
near-duplicate cluster found in a scan — each member's exact
`path:line` and job name, entirely offline, no dashboard required — when
two or more scanned jobs are near-duplicates of each other; this payload
field is what lets that same comparison happen fleet-wide, across every
repo in an org, instead of one scan at a time.

There's no real dashboard server yet to push to — this defines the
client contract for one that's a separate, later project (see the
`vlotpipe-dashboard` prototype in the sibling repo, which today pulls
data via `vlotpipe scan` + the GitHub API rather than receiving a push,
and its own `docs/adr/0001-push-ingestion-contract.md`, which commits to
this exact payload shape for whenever an ingestion endpoint gets built).
See also [`docs/adr/0001-selective-enforcement-and-report-push.md`](adr/0001-selective-enforcement-and-report-push.md)
for the design reasoning on this side.
Point `--report-to` at any endpoint that accepts this shape — internal
tooling, a webhook-to-spreadsheet integration, a real dashboard once one
exists — the client side doesn't care what's on the other end.

## A note on `.vlotpipe.yml` and file-list invocations

Both the `pre-commit` hook and the raw git hook invoke vlotpipe with an
explicit list of staged files rather than a directory — `vlotpipe scan
.github/workflows/ci.yml`, not `vlotpipe scan .`. `.vlotpipe.yml`
resolution handles this correctly: it walks upward from wherever
scanning started (the same way git/eslint/prettier find their own
config) until it finds either a `.vlotpipe.yml` or a `.git` directory
marking the repo root, so the repo's real exception list still applies
even though the file being linted is several directories below it.
