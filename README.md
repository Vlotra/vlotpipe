# vlotpipe

[![Go](https://img.shields.io/badge/go-1.26-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue)](LICENSE)
[![Tested against](https://img.shields.io/badge/tested_against-6_real_repos-brightgreen)](#tested-against)

**vlotpipe is a linter for CI pipelines — GitHub Actions and Azure
Pipelines.** ("ruff for pipelines, and yamllint for pipelines," if
you want the short version.)

Catch injection, credential, and supply-chain mistakes in your
pipelines before they run:

- An action pinned to a movable tag (`@v4`, `@main`) instead of a
  commit SHA, leading to **silent supply-chain compromise** if that tag
  ever gets repointed at a different commit.
- Untrusted PR code checked out inside a `pull_request_target`/
  `workflow_run`/`issue_comment` trigger, leading to **arbitrary code
  execution with your repo's secrets and write access**.
- Attacker-controlled input (an issue title, a PR body, a comment)
  interpolated straight into a `run:` shell script, leading to
  **arbitrary command execution via string injection** — no checkout
  required.
- A credential hardcoded into workflow YAML instead of referenced via
  `secrets.*`, leading to **permanent leakage** in git history.
- No `permissions:` block, or `secrets: inherit` on a reusable workflow
  call, leading to **an over-privileged runner**.

It's also a pipeline-aware YAML style linter — the half generic
`yamllint` can't really do, since it has no notion of GitHub Actions or
Azure Pipelines schema (its own `truthy` rule flags GitHub Actions' own
`on:` key by default; vlotpipe's equivalent never does, because it only
ever inspects values, not keys). And it isn't a binary "gate on
everything or nothing" tool: `--select` lets a team gate `check` on a
small, currently-clean set of rules today while still seeing the full
backlog on every scan — see
[`docs/SELECTIVE_ENFORCEMENT.md`](docs/SELECTIVE_ENFORCEMENT.md).

Every finding reports the way a modern linter should: instantly, with a
precise `file:line:col`, and with an exit code your CI can gate on.

```
$ vlotpipe scan .
.github/workflows/ci.yml:9:15: YAML005 "yes" is an unquoted YAML boolean-like value; not every YAML parser resolves it the same way — quote it ("yes") if a literal string was intended, or use true/false if a boolean was intended
.github/workflows/ci.yml:11:9: SEC001 action "actions/checkout" is pinned to "v4", not a full commit SHA
.github/workflows/ci.yml:13:9: SEC002 "api_key" looks like a hardcoded credential; use secrets.API_KEY instead
.github/workflows/ci.yml:20:5: TIMEOUT001 job 'build' has no timeout-minutes; defaults to GitHub's 360-minute cap

Found 4 violations across 1 file (2 blockers, 1 warning, 1 info)
```

New here? [`docs/GETTING_STARTED.md`](docs/GETTING_STARTED.md) walks
through install, your first scan, wiring `check` into CI, and
suppressing a finding — ten minutes, start to finish.
[`docs/INTEGRATIONS.md`](docs/INTEGRATIONS.md) covers adopting it
gradually: CI annotations, a drop-in GitHub Action, a `pre-commit`
framework hook, or a raw git hook — start with whichever has the least
friction for where a team is today.

## Tested against

Before shipping a rule, it gets run against a real, actively-maintained
pipeline and every finding — and every *absence* of a finding — gets
verified against the actual source, not trusted on faith. Six runs so
far, each picked to stress a different angle:

| Repo | Why it was picked | Result |
| --- | --- | --- |
| [astral-sh/ruff](https://github.com/astral-sh/ruff) | Already runs zizmor with documented `# zizmor: ignore[...]` trade-offs — an adjudicated ground truth to check against | 0 blockers; `SEC007` findings matched zizmor's own 1:1, exactly |
| [vitejs/vite](https://github.com/vitejs/vite) | Heavy bot/`pull_request_target` automation, also runs its own security scanners | Found a real gap (`issue_comment` missing from the dangerous-trigger list) — fixed |
| [szluyu99/gin-vue-blog](https://github.com/szluyu99/gin-vue-blog) | Deliberately picked for weaker CI hygiene, to check the signal actually separates cases | 12 blockers from 2 files — real functional CI, no SHA-pinning or permission scoping |
| [orbingol/aitos](https://github.com/orbingol/aitos) | Zero-star solo side project — contrast point against gin-vue-blog | 0 blockers — SHA-pinning tracks individual habit, not project fame |
| [dotnet/roslyn](https://github.com/dotnet/roslyn) (Azure Pipelines) | Large (548-line), mature, advanced-feature pipeline maintained by Microsoft | Found a real parser bug on nested conditional insertion — fixed |
| [AvaloniaUI/Avalonia](https://github.com/AvaloniaUI/Avalonia) (Azure Pipelines) | 31k+ stars, actively maintained, built partly by the team behind JetBrains Rider's UI | No timeout governance at all across the entire CI matrix — surfaced in under a second, zero config |

Full write-up for each run — method, every finding spot-checked, bugs
found and fixed along the way — in [`docs/VETTING_*.md`](docs). What
those runs found and fixed, as bugs in vlotpipe itself, is in
[`docs/TROPHY_CASE.md`](docs/TROPHY_CASE.md).

## Status

Early and evolving. GitHub Actions and Azure Pipelines are both
supported; GitLab CI is next. Expect the rule set and CLI surface to
change before v1.0. This repo has no tagged releases and no public
remote yet — build from source (below); `go install` isn't wired up
until that changes.

## Install

Build from source — the path that works today:

```
go build -o vlotpipe ./cmd/vlotpipe
```

Once this repo is published with tagged releases, this will also work:

```
go install github.com/vlotra/vlotpipe/cmd/vlotpipe@latest
```

## Usage

```
vlotpipe scan [paths...]    # report every violation, always exits 0
vlotpipe check [paths...]   # same scan, but exits 1 if a blocker is found — use this in CI
vlotpipe init [dir]         # write a starter .vlotpipe.yml + a self-check CI workflow
vlotpipe format [paths...]  # fix indentation to match structural nesting depth (see below)
```

Both commands default to scanning `.` and walk it for both
`.github/workflows/*.yml` (GitHub Actions) and `azure-pipelines.yml`
(Azure Pipelines, matched by filename at any directory depth — Azure has
no fixed folder convention the way GitHub Actions does), skipping
vendored directories (`node_modules`, `vendor`, `.venv`, `dist`,
`build`, ...) and nested git submodules — a submodule's `.git` is a
gitlink file rather than a directory, which is how it's detected
regardless of the directory name.

Flags:

| Flag              | Description                                              |
| ----------------- | --------------------------------------------------------- |
| `--format`        | `text` (default), `json`, `github`, or `azure-devops` (auto-detected from the CI environment if omitted) |
| `--severity`      | minimum severity to report: `blocker`, `warning`, `info`  |
| `--config`        | directory containing `.vlotpipe.yml` (default: first scanned path) |
| `--fix`           | auto-fix the small subset of violations with a safe, mechanical fix |
| `--statistics`/`-s` | print an aggregate summary (by severity, by rule, by file — worst file first) instead of the raw violation list; useful when scanning many repos/files at once |
| `--select`        | comma-separated rule codes/prefixes that can fail `check` (default: every rule); see [`docs/SELECTIVE_ENFORCEMENT.md`](docs/SELECTIVE_ENFORCEMENT.md) |
| `--report-select` | comma-separated rule codes/prefixes to include in output (default: every rule); independent of `--select` |
| `--report-to`     | push the complete, unfiltered finding set to this URL after scanning; see [`docs/INTEGRATIONS.md`](docs/INTEGRATIONS.md#5-pushing-to-a-dashboard) |

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
  hmak-web2/.github/workflows/security-scan.yml  blockers=31 warnings=8 info=1
  ...
```

## Auto-fixing and formatting

Two different tools for two different jobs — pick based on how much
diff you're willing to accept.

**`--fix`** (on `scan`/`check`) makes small, targeted text edits for the
handful of rules with a safe, unambiguous fix — inserting one new line,
never touching anything else. A rule only gets an autofix if applying it
needs no judgment call (never guessing at a real timeout value's
"right" number beyond a clearly-labeled placeholder, never touching a
secret). Currently: `TIMEOUT001`, `AZR001`, `SEC006`, `LEAN011`,
`PERF002` — each rule's own page under [`docs/rules/`](docs/rules/README.md)
says exactly what its fix does and notes "Autofix: yes" where it
applies.

```
$ vlotpipe check . --fix
fixed 4 violation(s)
.github/workflows/ci.yml:9:3: SEC005 job 'build' has no permissions: set...
```

**`vlotpipe format`** is a different trade-off entirely: a surgical text
patch that rewrites only the lines whose leading whitespace disagrees
with their structural nesting depth — never key order, never blank
lines, never comments or quote style. A file with one
inconsistently-indented block produces a diff scoped to that block, not
a whole-file rewrite (see
[`docs/adr/0003-surgical-indent-fixer.md`](docs/adr/0003-surgical-indent-fixer.md)).
`--check` lists files that would change and exits 1 without writing,
for a CI gate on style rather than content:

```
vlotpipe format .            # fix indentation in place
vlotpipe format . --check    # list what would change, exit 1 if anything would
```

`--reorder-keys` opts into the older, bigger trade-off instead: a full
parse-and-re-encode pass, the way `gofmt` treats Go source — canonical
key order (workflow/pipeline root, then job, then step) and a
consistent 2-space indent, applied throughout the whole file. That's a
much bigger diff, and on first run against a hand-formatted file it will
look disruptive: it does not preserve blank lines between blocks (the
underlying YAML representation doesn't track them at all), though
comments do survive.

## Rules

Rule codes follow a ruff-style namespace: `<CATEGORY><NNN>`. `YAML`
covers pipeline-aware YAML style and syntax — the yamllint half, see
[`docs/rules/README.md`](docs/rules/README.md#why-a-yaml-category-exists)
for why a pipeline-aware version catches things generic yamllint can't
(and stays quiet on things it wrongly flags, like GitHub Actions' own
`on:` key). `SEC` covers GitHub Actions security, `AZR` covers Azure
Pipelines-specific concerns, `SUPPLY` covers supply-chain integrity
(GitHub-only — see why in `docs/AZURE_RESEARCH.md`), `LEAN` covers
pipeline files/build times staying short, `PERF`/`TIMEOUT`/`STRUCT`
cover performance, reliability, and structure. Every `YAML` rule and
most structural rules are platform-neutral; `SEC016`, `PERF001`,
`LEAN001`, `LEAN008`, `STRUCT001`, and `STRUCT002` also run against
both platforms — see `docs/SECURITY_RESEARCH.md`, `docs/LEAN_PIPELINES_RESEARCH.md`,
and `docs/AZURE_RESEARCH.md` for the research (and, for Azure, which
GitHub rules deliberately have no equivalent, and why) behind each rule.

| Code       | Severity | Checks                                                        |
| ---------- | -------- | -------------------------------------------------------------- |
| YAML001    | blocker  | file is not valid YAML                                          |
| YAML002    | warning  | duplicate key in the same mapping (silently overwritten, not merged) |
| YAML003    | info     | trailing whitespace                                              |
| YAML004    | info     | missing or extra newline(s) at end of file                      |
| YAML005    | info     | unquoted YAML 1.1 boolean-like value (`yes`/`no`/`on`/`off`/`y`/`n`) |
| SEC001     | blocker  | action not pinned to a full commit SHA                        |
| SEC002     | blocker  | `with`/`env` value that looks like a hardcoded credential      |
| SEC003     | blocker  | `pull_request_target`/`workflow_run` checks out the untrusted PR/run head |
| SEC004     | blocker  | untrusted context (PR title, issue body, ...) interpolated into `run:` |
| SEC005     | warning  | no `permissions:` set at workflow or job level                 |
| SEC006     | warning  | `actions/checkout` without `persist-credentials: false`        |
| SEC007     | warning  | `secrets: inherit` on a reusable workflow call                 |
| SEC008     | warning  | `toJSON(secrets)` dumps the entire secrets context              |
| SEC010     | warning  | self-hosted-looking runner in a workflow triggered by `pull_request` |
| SEC011     | warning  | spoofable `github.actor ==` authorization check                |
| SEC012     | blocker  | `GITHUB_ENV`/`GITHUB_PATH` write with untrusted input on a dangerous trigger |
| SEC013     | warning  | `ACTIONS_ALLOW_UNSECURE_COMMANDS` re-enables deprecated, injectable commands |
| SEC014     | blocker  | hardcoded `container`/`services` registry credentials          |
| SEC015     | warning  | GitHub Actions cache restored in a `release`-triggered workflow |
| SEC016     | warning  | step dumps the entire environment (`printenv`/`env`/`set` with no args) to the log |
| AZR001     | warning  | Azure job with no `timeoutInMinutes` (defaults to 60 min on Microsoft-hosted agents) |
| AZR002     | blocker  | hardcoded credential in an Azure task's `inputs:`/`env:`        |
| SUPPLY001  | warning  | no Dependabot/Renovate config to keep pinned actions updated (repo-level) |
| PERF001    | warning¹ | dependency install step with no matching cache step             |
| PERF002    | info     | no `concurrency:` group on a `pull_request`-triggered workflow  |
| PERF003    | info     | action wraps a CLI already on the runner (e.g. `dtolnay/rust-toolchain` vs `rustup`) |
| LEAN001    | warning  | installs a language runtime via `apt-get`/`apk`/`yum` instead of a `setup-*` action or image |
| LEAN002    | info     | `fetch-depth: 0` with no step that appears to need git history  |
| LEAN008    | info     | two jobs share the same first 3 steps, copy-pasted instead of extracted |
| LEAN010    | warning¹ | Docker build step (`docker/build-push-action`) with no cache-from/cache-to |
| LEAN011    | info     | `actions/upload-artifact` with no `retention-days` set          |
| TIMEOUT001 | warning  | job with no `timeout-minutes` set                              |
| STRUCT001  | info     | no job name suggests a test/lint/check step runs                |
| STRUCT002  | info     | job has more than 20 steps (configurable), a bloat/tidiness signal |

¹ Downgraded to `info` when `runs-on:` doesn't look GitHub-hosted or
like a known managed-ephemeral runner service — the "starts from
nothing every run" assumption behind these two doesn't hold on a
persistent self-hosted worker, which may already have the cache on
local disk with no `actions/cache`/`cache-from` step in sight.

This table is a summary — [`docs/rules/`](docs/rules/README.md) has the
full, always-current list, one page per rule with examples.

## Suppressing a violation

Two ways, for two different scopes.

**Repo-wide, with a reason on record** — add a `.vlotpipe.yml` at the repo
root (`vlotpipe init` writes a starter one). Every exception needs a
reason; `expires` is optional but recommended so suppressions don't
outlive their justification.

```yaml
ignore:
  - code: STRUCT001
    path: "*"
    reason: "job naming convention doesn't include test/lint yet, tracked in VLOT-42"
    expires: "2026-12-31"
```

**One line, inline** — a trailing comment in the workflow file itself,
same syntax as [zizmor](https://docs.zizmor.sh)'s
`# zizmor: ignore[...]`, which several real-world repos vetted while
building this tool (ruff, vite) already use that pattern for exactly
this. No `.vlotpipe.yml` entry needed:

```yaml
secrets: inherit # vlotpipe: ignore[SEC007]
uses: some/action@v1 # vlotpipe: ignore
```

A bracketed list (`ignore[SEC007]`, or comma-separated for several
codes) suppresses just those codes on that line; a bare `# vlotpipe:
ignore` suppresses everything found on that line.

**A third, different mechanism** — `select`/`report.select` in
`.vlotpipe.yml` isn't suppression (nothing gets marked "not a real
problem"); it narrows *which rule codes can fail `check`* independently
of what's still reported, so a repo can gate on a small, currently-clean
set today while still seeing its full backlog in every scan. See
[`docs/SELECTIVE_ENFORCEMENT.md`](docs/SELECTIVE_ENFORCEMENT.md).

## Custom rules

The baseline pack is fixed at compile time, but org-specific policy
doesn't have to fork the repo to be enforced — declare it in
`.vlotpipe.yml` instead:

```yaml
custom_rules:
  - code: ORG001
    severity: warning
    scope: job
    field: timeout-minutes
    equals: "360"
    message: "timeout-minutes is set to GitHub's own default; pick a real bound"
```

That example is the motivating one: `TIMEOUT001` only checks that a
timeout is *set*, so a job that sets `timeout-minutes: 360` (GitHub's
own default) technically satisfies it while missing the point entirely.
See [`docs/CUSTOM_RULES.md`](docs/CUSTOM_RULES.md) for the full field
list, matcher reference, and what this mechanism deliberately doesn't
try to cover.

## Project layout

```
cmd/vlotpipe/            CLI entrypoint (cobra commands: scan, check, init, format)
internal/model/          normalized pipeline AST (stages/jobs/steps) every parser maps into
internal/parser/github/  GitHub Actions YAML -> internal/model
internal/parser/azure/   Azure Pipelines YAML -> internal/model
internal/parser/suppress/ shared "# vlotpipe: ignore" comment scanner, used by every parser
internal/rules/          rule engine (Rule interface, registry, severity)
internal/rules/baseline/ the rule pack (SEC/AZR/SUPPLY/LEAN/PERF/TIMEOUT/STRUCT)
internal/rules/repolevel/ checks that need repo layout beyond one workflow file (SUPPLY001)
internal/customrules/    .vlotpipe.yml custom_rules: evaluator — org policy without a rebuild
internal/fingerprint/    near-duplicate job detection (simhash) — local teaser + report-to payload, see docs/adr/0004
internal/fixer/          "--fix": small, targeted text edits for a curated, safe subset of rules
internal/formatter/      "vlotpipe format": total re-encode with canonical key order + indent
internal/yamllint/       YAML* rules: syntax validity, duplicate keys, whitespace, truthy values
internal/report/         terminal + JSON formatters, plus GitHub/Azure DevOps annotation formats
internal/pushreport/     "--report-to": pushes the complete finding set to a dashboard endpoint
internal/config/         .vlotpipe.yml exception + custom-rule loading, `vlotpipe init` template
testdata/github/         good/bad GitHub Actions fixtures used by rule tests
testdata/azure/          good/bad Azure Pipelines fixtures used by rule tests
action.yml               composite GitHub Action wrapping install + run + annotate
.pre-commit-hooks.yaml   hook definitions for the pre-commit framework (pre-commit.com)
scripts/pre-commit       raw git pre-commit hook, no framework dependency
docs/adr/                architecture decision records — why, not just what
```

## License

MIT for the CLI and core rule engine. See [LICENSE](LICENSE).
