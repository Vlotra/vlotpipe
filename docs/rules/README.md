# vlotpipe rules

Every rule vlotpipe ships, one page each: what it checks, why, a flagged
example and a fixed one, and how to suppress it if you need to. All
examples on these pages are original — written for this documentation,
not reproduced from any other tool's docs or advisories.

vlotpipe supports two platforms: **GitHub Actions**
(`.github/workflows/*.yml`) and **Azure Pipelines**
(`azure-pipelines.yml`, any directory depth). Most rules are GitHub-only
(marked below); a handful of structural checks, `PERF001`, and `SEC016`
are platform-neutral and run against both; `AZR*` codes are Azure-only. See
[`docs/AZURE_RESEARCH.md`](../AZURE_RESEARCH.md) for why several
GitHub-only rules — `SEC001` (no SHA-pin equivalent for Azure tasks),
`SEC006` (Azure's checkout already defaults the safe way), `LEAN002`
(Azure's shallow-fetch default isn't visible in the YAML) — deliberately
don't have Azure counterparts.

Rule codes follow `<CATEGORY><NNN>`. Categories:

- **YAML** — pipeline-aware YAML style and syntax, independent of any policy: is it valid, is it internally consistent — the "yamllint for pipelines" half of vlotpipe, see below
- **SEC** — GitHub Actions security (secrets, supply-chain integrity, injection, token scope)
- **AZR** — Azure Pipelines-specific (its own security/reliability concerns)
- **SUPPLY** — supply-chain tooling (keeping pinned dependencies current) — GitHub-only
- **PERF** — runtime performance (caching, concurrency, redundant work)
- **LEAN** — pipeline files and build times staying short
- **TIMEOUT** — job/step time bounds — GitHub-only (see `AZR001` for Azure)
- **STRUCT** — pipeline structure and completeness — platform-neutral
- **DUP** — near-duplicate jobs, possibly across different files — platform-neutral; see [`DUP001.md`](DUP001.md). Unlike every other category, a `DUP001` finding isn't a `rules.Violation` under the hood and doesn't appear in `--format json`/`github`/`azure-devops` output — see the page for why.

### Why a YAML category exists

Generic `yamllint` has no notion of GitHub Actions or Azure Pipelines
schema, which cuts both ways: it misses pipeline-specific context, and
it also produces false noise a pipeline-aware checker doesn't have to.
The clearest example is yamllint's own `truthy` rule, which by default
flags GitHub Actions' own required `on:` trigger key as an ambiguous
boolean-like key — real projects carry a custom yamllint config just to
silence that one key. `YAML005` only ever inspects mapping *values*,
never keys, so `on:` is never a candidate to begin with; nothing to
special-case. See [`internal/yamllint/`](../../internal/yamllint/yamllint.go)
for the implementation — these checks run on the raw YAML tree directly
rather than the normalized `internal/model`, since duplicate keys and
quoting style exist below the level the model captures, and apply
identically regardless of platform.

Every finding can be suppressed either per-repo in `.vlotpipe.yml` or
inline with a `# vlotpipe: ignore[CODE]` comment — see each page's
"Suppressing" section, or the main [README](../../README.md#suppressing-a-violation).

Not on this list yet — write your own via [`custom_rules:` in
`.vlotpipe.yml`](../CUSTOM_RULES.md), no fork or rebuild required.

A rule marked **Autofix: yes** below can be applied automatically with
`vlotpipe scan/check --fix` — see
[`docs/GETTING_STARTED.md`](../GETTING_STARTED.md#auto-fixing-and-formatting)
for what that does and doesn't cover. Only five rules have one; the rest
need a human call.

Every rule can also be tuned per repo via `.vlotpipe.yml`'s `rules:`
section, keyed by exact code — two properties are generic and apply to
every rule the same way, documented here once rather than on each page:

- **`severity: blocker|warning|info`** — replaces the rule's shipped
  severity everywhere it's consulted (display, the `check` gate, and
  `report.to`), not just how it's colored in text output.
- **`fix: false`** — for a rule that's normally autofixable, skip the
  automatic edit only; the finding still fires and gets reported as
  usual.

A handful of rules also take a *special* property, meaningful only to
that one rule — those are documented on that rule's own page instead
(`STRUCT002.max_steps`, `PERF001`/`LEAN010.cached_runners`,
`TIMEOUT001`/`AZR001.fix_default`). See
[`docs/adr/0002-rule-specific-runner-config.md`](../adr/0002-rule-specific-runner-config.md)
for the full reasoning behind the split.

## YAML

| Code | Severity | Autofix | Checks |
| --- | --- | --- | --- |
| [YAML001](YAML001.md) | blocker | | file is not valid YAML |
| [YAML002](YAML002.md) | warning | | duplicate key in the same mapping (silently overwritten, not merged) |
| [YAML003](YAML003.md) | info | | trailing whitespace |
| [YAML004](YAML004.md) | info | | missing or extra newline(s) at end of file |
| [YAML005](YAML005.md) | info | | unquoted YAML 1.1 boolean-like value (`yes`/`no`/`on`/`off`/`y`/`n`) |

## Security

| Code | Severity | Autofix | Checks |
| --- | --- | --- | --- |
| [SEC001](SEC001.md) | blocker | | action not pinned to a full commit SHA |
| [SEC002](SEC002.md) | blocker | | hardcoded credential in `with`/`env` |
| [SEC003](SEC003.md) | blocker | | `pull_request_target`/`workflow_run`/`issue_comment` checks out the untrusted head |
| [SEC004](SEC004.md) | blocker | | untrusted input interpolated directly into `run:` |
| [SEC005](SEC005.md) | warning | | no `permissions:` set at workflow or job level |
| [SEC006](SEC006.md) | warning | yes | `actions/checkout` without `persist-credentials: false` |
| [SEC007](SEC007.md) | warning | | `secrets: inherit` on a reusable workflow call |
| [SEC008](SEC008.md) | warning | | `toJSON(secrets)` dumps the entire secrets context |
| [SEC010](SEC010.md) | warning | | self-hosted-looking runner in a fork-triggerable workflow |
| [SEC011](SEC011.md) | warning | | spoofable `github.actor ==` authorization check |
| [SEC012](SEC012.md) | blocker | | `GITHUB_ENV`/`GITHUB_PATH` write with untrusted input on a dangerous trigger |
| [SEC013](SEC013.md) | warning | | re-enabled deprecated, injectable workflow commands |
| [SEC014](SEC014.md) | blocker | | hardcoded container/service registry credentials |
| [SEC015](SEC015.md) | warning | | cache restored inside a release-triggered workflow |
| [SEC016](SEC016.md) | warning | | step dumps the entire environment to the log (GitHub + Azure) |

## Azure Pipelines

| Code | Severity | Autofix | Checks |
| --- | --- | --- | --- |
| [AZR001](AZR001.md) | warning | yes | job with no `timeoutInMinutes` set (defaults to 60 min on Microsoft-hosted agents) |
| [AZR002](AZR002.md) | blocker | | hardcoded credential in a task's `inputs:`/`env:` |

## Supply chain

| Code | Severity | Autofix | Checks |
| --- | --- | --- | --- |
| [SUPPLY001](SUPPLY001.md) | warning | | no Dependabot/Renovate config to keep pinned actions updated |

## Performance

| Code | Severity | Autofix | Checks |
| --- | --- | --- | --- |
| [PERF001](PERF001.md) | warning → info on self-hosted-looking runners | | dependency install without a matching cache step (GitHub + Azure) |
| [PERF002](PERF002.md) | info | yes | no `concurrency:` group on a `pull_request`-triggered workflow |
| [PERF003](PERF003.md) | info | | action wraps a CLI already available on the runner |

## Lean pipelines

| Code | Severity | Autofix | Checks |
| --- | --- | --- | --- |
| [LEAN001](LEAN001.md) | warning | | installs a language runtime via the package manager every run (GitHub + Azure) |
| [LEAN002](LEAN002.md) | info | | `fetch-depth: 0` with no step that appears to need git history |
| [LEAN008](LEAN008.md) | info | | two jobs share the same first 3 steps, copy-pasted instead of extracted (GitHub + Azure) |
| [LEAN010](LEAN010.md) | warning → info on self-hosted-looking runners | | Docker build step with no cache-from/cache-to backend |
| [LEAN011](LEAN011.md) | info | yes | uploaded artifact with no `retention-days` set |

## Timeouts & structure

| Code | Severity | Autofix | Checks |
| --- | --- | --- | --- |
| [TIMEOUT001](TIMEOUT001.md) | warning | yes | job with no `timeout-minutes` set |
| [STRUCT001](STRUCT001.md) | info | | no job name suggests a test/lint/check step runs (GitHub + Azure) |
| [STRUCT002](STRUCT002.md) | info | | job has more than 20 steps (configurable) — pipeline bloat/tidiness (GitHub + Azure) |

## DUP

| Code | Severity | Autofix | Checks |
| --- | --- | --- | --- |
| [DUP001](DUP001.md) | info | | job is a near-duplicate (≥90% structural similarity) of another job found in this scan (GitHub + Azure) |

## Research behind these rules

- The `YAML` category follows the well-established conventions of [yamllint](https://yamllint.readthedocs.io/), the standard generic YAML linter — `duplicate-key`, `trailing-spaces`, `new-line-at-end-of-file`, and `truthy` are all yamllint rules by name. What's different here is scope, not novelty: `YAML005` only inspects values, never keys, so it doesn't need the config carve-out real projects add to silence yamllint's default flagging of GitHub Actions' own `on:` key.
- [`docs/SECURITY_RESEARCH.md`](../SECURITY_RESEARCH.md) — the `SEC`/`SUPPLY` category research: OWASP's CI/CD Top 10, zizmor's audit catalog, and real incidents (tj-actions/changed-files, ArtiPACKED) behind each choice.
- [`docs/LEAN_PIPELINES_RESEARCH.md`](../LEAN_PIPELINES_RESEARCH.md) — the `LEAN`/`PERF` category research.
- [`docs/AZURE_RESEARCH.md`](../AZURE_RESEARCH.md) — what ports from GitHub Actions to Azure Pipelines and what doesn't, with the verified Azure YAML schema facts behind each `AZR*` rule.
- [`docs/VETTING_*.md`](..) — real-world runs against ruff, vite, gin-vue-blog, aitos, dotnet/roslyn, and AvaloniaUI/Avalonia, including bugs found and fixed along the way (`SEC016` itself came out of the Avalonia run).
