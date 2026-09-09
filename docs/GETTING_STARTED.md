# Getting started

vlotpipe is two linters in one pass: a policy linter (`SEC`/`AZR`/
`PERF`/`LEAN`/`STRUCT` — security, performance, structure) and a
pipeline-aware YAML style linter (`YAML` — syntax validity, duplicate
keys, ambiguous unquoted values), the "yamllint for pipelines" half
that a generic YAML linter can't really do, since it has no notion of
GitHub Actions or Azure Pipelines schema.

This walks through installing vlotpipe, running your first scan, wiring
it into CI so it actually gates something, and the two ways to tell it
"I know about this one, leave it alone." Ten minutes, start to finish.

## Install

Build from source — this is the path that works today, this repo isn't
published anywhere yet:

```
git clone <this repo>
cd vlotpipe
go build -o vlotpipe ./cmd/vlotpipe
./vlotpipe --version   # "vlotpipe version <v> (commit <sha>, built <date>)" — confirms the build
./vlotpipe --help
```

Requires Go 1.26+ (see `go.mod`). Once this repo has a public remote and
tagged releases, `go install github.com/vlotra/vlotpipe/cmd/vlotpipe@latest`
will be the one-liner — not yet, so don't reach for it before then.

Put the binary somewhere on your `PATH` (`mv vlotpipe /usr/local/bin/`,
or wherever you keep local tools) so the rest of this guide can just say
`vlotpipe`.

## Your first scan

From the root of a repo that has GitHub Actions workflows
(`.github/workflows/*.yml`) or an Azure Pipelines file
(`azure-pipelines.yml`, any directory depth):

```
$ vlotpipe scan .
.github/workflows/ci.yml:11:9: SEC001 action "actions/checkout" is pinned to "v4", not a full commit SHA
.github/workflows/ci.yml:13:9: SEC002 "api_key" looks like a hardcoded credential; use secrets.API_KEY instead
.github/workflows/ci.yml:20:5: TIMEOUT001 job 'build' has no timeout-minutes; defaults to GitHub's 360-minute cap

Found 3 violations across 1 file (2 blockers, 1 warning, 0 infos)
```

Every line is `file:line:col: CODE message` — click through straight to
the offending line in most editors and terminals. `scan` always exits 0;
it's for looking, not gating (see [`check`](#gating-ci-on-it) below).

Scanning a big monorepo with a lot of noise? Reach for `-s`/`--statistics`
for an aggregate view instead of the raw list:

```
$ vlotpipe scan . -s
24 files scanned, 229 violations found

By severity
  blocker   114
  warning   95
  info      20

By rule
  ● SEC001      114
  ● TIMEOUT001  36
  ...

By file (worst first)
  services/payments/.github/workflows/deploy.yml  blockers=31 warnings=8 info=1
  ...
```

That's usually the faster way to decide where to start: fix the worst
file first, or knock out the highest-count rule across the whole repo in
one pass.

## Gating CI on it

`vlotpipe check` runs the same scan but exits 1 if any **blocker**-severity
violation is found — that's the one to put in a pipeline:

**GitHub Actions:**

```yaml
- name: Lint CI pipelines
  run: |
    curl -sL <release-url>/vlotpipe -o vlotpipe && chmod +x vlotpipe
    ./vlotpipe check .
```

**Azure Pipelines:**

```yaml
- script: |
    curl -sL <release-url>/vlotpipe -o vlotpipe && chmod +x vlotpipe
    ./vlotpipe check .
  displayName: 'Lint CI pipelines'
```

(No published binary to `curl` yet, either — see [Install](#install).
Once releases exist, this step is a two-line drop-in for either
platform, which is also a decent trick: use vlotpipe to lint the very
pipeline file that's running it.)

`--severity` controls the gate's threshold if blocker-only is too loose
or too strict for a given repo:

```
vlotpipe check . --severity warning   # fail on warning or worse, not just blocker
```

Turning `check` into a required status check on a repo with existing
findings can mean failing every open PR at once. `select` in
`.vlotpipe.yml` (or `--select`) narrows *which rule codes* can actually
fail the build, independent of what still gets reported — gate on a
small, currently-clean set today, see the full backlog in every scan so
you know what to widen the gate to next. See
[`docs/SELECTIVE_ENFORCEMENT.md`](SELECTIVE_ENFORCEMENT.md) for the full
story.

## Not everything needs fixing today

Two ways to tell vlotpipe "seen it, leave it" — pick the scope that
fits.

**One line, inline** — for a single occurrence, right where it happens.
No config file needed:

```yaml
secrets: inherit # vlotpipe: ignore[SEC007]
uses: some/action@v1 # vlotpipe: ignore
```

**Repo-wide, with a reason on record** — for something that applies
everywhere, or that needs a paper trail for why it's suppressed. Start
with:

```
vlotpipe init
```

which writes a starter `.vlotpipe.yml`:

```yaml
ignore:
  - code: STRUCT001
    path: "*"
    reason: "job naming convention doesn't include test/lint yet, tracked in VLOT-42"
    expires: "2026-12-31"
```

`reason` and `expires` are both optional, but recommended — `reason` so
the next person to read this file knows why, `expires` so a suppression
doesn't quietly outlive the reason it was added. When neither matters
(a quick local silence, no paper trail needed), a bare code string is
shorthand for the same entry with no reason, applied to every path:

```yaml
ignore:
  - SEC001
  - TIMEOUT001
```

Full syntax (path globs, code lists, mixing shorthand and full-object
entries in the same list) in the main
[README](../README.md#suppressing-a-violation).

**Duplicate-job findings (`DUP001`) suppress the same two ways** — inline
on the job's key line, or repo-wide in `.vlotpipe.yml`:

```yaml
jobs:
  test-frontend: # vlotpipe: ignore[DUP001]
```

```yaml
ignore:
  - code: DUP001
    path: "dast.yml"
    reason: "shared guard clause, not a real duplicate — see docs/rules/DUP001.md"
```

See [`docs/rules/DUP001.md`](rules/DUP001.md) for what this rule
actually flags — unlike every other rule, it can span two different
files, since a "duplicate" is a relationship between two jobs rather
than a property of one.

## Org-specific policy

The baseline rule pack is fixed at compile time, but that doesn't mean a
company-specific rule needs a fork. The same `.vlotpipe.yml` also takes
`custom_rules:` — a small declarative DSL for policy the baseline pack
can't know about (an internal action that must always be pinned to a
specific org registry, a `timeout-minutes` value that's technically set
but is actually just GitHub's own unhelpful default, etc.). See
[`docs/CUSTOM_RULES.md`](CUSTOM_RULES.md) for the field reference and
worked examples.

## Auto-fixing and formatting

`--fix` (on `scan`/`check`) applies small, targeted edits for the
handful of rules with a safe, unambiguous fix — one new line inserted,
nothing else touched:

```
$ vlotpipe check . --fix
fixed 4 violation(s)
```

Only five rules currently have one (`TIMEOUT001`, `AZR001`, `SEC006`,
`LEAN011`, `PERF002` — each rule's own page under `docs/rules/` says
what its fix does). A rule earns an autofix only when applying it needs
no judgment call; most rules don't and never will.

`rules:` in `.vlotpipe.yml` tunes the two rules that insert a value
rather than a fixed key — useful if a team wants `timeout-minutes` to
stay a deliberate decision instead of a number `--fix` picked. It's a
general per-code config section (not just for `--fix`) — see
[`docs/adr/0002-rule-specific-runner-config.md`](adr/0002-rule-specific-runner-config.md)
for the full shape, including `severity` overrides and other rules'
special properties:

```yaml
rules:
  TIMEOUT001:
    fix: false          # still fires and gets reported — just never auto-fixed
    fix_default: 15     # override the value --fix inserts (default: 30)
  AZR001:
    fix_default: 15     # independent of TIMEOUT001's own fix_default
```

`vlotpipe format` fixes indentation only, by default — a surgical text
patch that rewrites just the lines whose leading whitespace disagrees
with their structural nesting depth. A file with one inconsistently
indented block produces a diff scoped to that block; blank lines,
comments, quote style, and key order are all left exactly as they were.
`--check` lists what would change and exits 1 without writing, for a
style gate separate from `check`'s content gate:

```
vlotpipe format .            # fix indentation in place
vlotpipe format . --check    # list what would change, exit 1 if anything would
```

`--reorder-keys` opts into the older, bigger trade-off instead: a full
parse-and-re-encode pass, canonical key order plus indentation
throughout the whole file, the way `gofmt` treats Go source. That means
a large diff on the first run against a hand-formatted file (blank
lines between blocks don't survive that pass; comments do) — see
[`docs/adr/0003-surgical-indent-fixer.md`](adr/0003-surgical-indent-fixer.md)
for why the surgical pass is the default instead.

## Where to go next

- [`docs/rules/README.md`](rules/README.md) — every rule vlotpipe ships, one page each, with a flagged/fixed example, whether it has an autofix, and how to suppress it specifically.
- [`docs/INTEGRATIONS.md`](INTEGRATIONS.md) — CI annotations, a drop-in GitHub Action, a `pre-commit` hook, a raw git hook, pushing to a dashboard: adopting this gradually instead of all at once.
- [`docs/SELECTIVE_ENFORCEMENT.md`](SELECTIVE_ENFORCEMENT.md) — `select`/`report.select`: gate on a small rule set while still seeing the full backlog.
- [`docs/CUSTOM_RULES.md`](CUSTOM_RULES.md) — the `custom_rules:` DSL.
- [`docs/SECURITY_RESEARCH.md`](SECURITY_RESEARCH.md) / [`docs/LEAN_PIPELINES_RESEARCH.md`](LEAN_PIPELINES_RESEARCH.md) / [`docs/AZURE_RESEARCH.md`](AZURE_RESEARCH.md) — the research behind each rule category, including what deliberately *isn't* a rule and why.
- [`docs/TROPHY_CASE.md`](TROPHY_CASE.md) — real bugs found and fixed by running vlotpipe against real, actively-maintained pipelines.

## Full CLI reference

```
vlotpipe scan [paths...]    # report every violation, always exits 0
vlotpipe check [paths...]   # same scan, but exits 1 if a blocker is found — use this in CI
vlotpipe init [dir]         # write a starter .vlotpipe.yml + a self-check CI workflow
vlotpipe format [paths...]  # fix indentation to match structural nesting depth (surgical by default)
```

Flags on `scan`/`check`:

| Flag | Description |
| --- | --- |
| `--format` | output format: `text` (default), `json`, `github`, or `azure-devops` (auto-detected from the CI environment if omitted — see `docs/INTEGRATIONS.md`) — unrelated to the `format` command above, despite the shared name |
| `--severity` | minimum severity to report/gate on: `blocker`, `warning`, `info` |
| `--config` | directory containing `.vlotpipe.yml` (default: first scanned path) |
| `--fix` | auto-fix the small subset of violations with a safe, mechanical fix |
| `--statistics` / `-s` | aggregate summary instead of the raw violation list |
| `--select` | comma-separated rule codes/prefixes that can fail `check` (default: every rule); overrides `.vlotpipe.yml`'s `select:` — see `docs/SELECTIVE_ENFORCEMENT.md` |
| `--report-select` | comma-separated rule codes/prefixes to include in output (default: every rule); independent of `--select`, overrides `report.select:` |
| `--report-to` | push the complete, unfiltered finding set to this URL after scanning; overrides `VLOTPIPE_REPORT_TO` and `report.to:` — see `docs/INTEGRATIONS.md` |

Flag on `format`:

| Flag | Description |
| --- | --- |
| `--check` | list files that would change, without writing; exit 1 if any would |
| `--reorder-keys` | full parse-and-re-encode pass (canonical key order + indent throughout) instead of the default surgical indent-only fix — bigger diff, see above |

All commands default to scanning `.` if no paths are given, and walk it
for both `.github/workflows/*.yml` and `azure-pipelines.yml` (any
directory depth), automatically skipping vendored directories
(`node_modules`, `vendor`, `.venv`, `dist`, `build`, ...) and nested git
submodules.
