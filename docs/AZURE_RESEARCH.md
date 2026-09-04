# Azure Pipelines research — what ports from GitHub Actions and what doesn't

vlotpipe's second platform, and the first real test of whether the
"normalized internal model" design (documented in `internal/model`
since the project's first commit) actually holds up: does a rule
written against `model.Job`/`model.Step` work unchanged for a
completely different CI system, or does every rule secretly assume
GitHub Actions?

Answer: partially. Structural rules (job naming, step count, duplicate
step prefixes) ported with zero logic changes — only their *message
wording* needed a platform branch, since "composite action or reusable
workflow" isn't a term Azure users would recognize (their equivalent is
a "template"). Security- and performance-flavored rules mostly didn't
port, because the two platforms' underlying models — how a task gets
its version, whether a checkout persists credentials by default, what a
job's default timeout even is — are often *opposite*, not just
differently named. Getting that distinction right, rather than
naively reusing a GitHub-shaped rule with Azure vocabulary swapped in,
was the actual point of this research pass.

## Verified facts (sources: Microsoft Learn YAML schema reference)

**Job timeout defaults to 60 minutes on Microsoft-hosted agents**
(unlimited on self-hosted), confirmed across multiple independent
sources. This is a different number from GitHub Actions' 360-minute
default — `AZR001` needed to be its own rule with its own message, not
a shared `TIMEOUT001` with an if-branch, so the fact stated is never
platform-ambiguous.

**`checkout`'s `persistCredentials` defaults to *not* persisting** —
the literal opposite of GitHub Actions' `actions/checkout`, which
persists by default (the reason `SEC006` exists at all). This means
Azure's checkout step doesn't need a `SEC006`-shaped rule: the safe
behavior is already the default, so there's nothing to nudge users
toward. Porting `SEC006` as-is would have told Azure users to add a
setting they already have implicitly.

**Task versioning has no SHA-pin equivalent.** GitHub Actions'
`uses: owner/repo@ref` can reference anything from a full commit SHA
(immutable) to a mutable branch tag — `SEC001` exists because that
range includes genuinely unsafe choices. Azure's `task: Name@N` syntax
*requires* a major version number by platform convention, and
Microsoft's own docs state the platform **auto-updates minor
versions** within that major version, centrally, across the whole
organization. There's no equivalent of "an attacker moves the tag";
the versioning is centrally managed by Azure DevOps, not a mutable git
ref an external party controls. No `SEC001` analog exists for Azure —
recommending "pin further" wouldn't even be actionable in the standard
task-authoring model.

**`fetchDepth`'s default is UI-configurable, not a fixed YAML default.**
Unlike GitHub Actions (where `actions/checkout` unambiguously defaults
to `fetch-depth: 1`), Azure's shallow-fetch behavior is controlled by a
pipeline-settings-UI toggle that isn't visible in the YAML file at all
— `fetchDepth` in the YAML only overrides that setting when explicitly
present. A static scanner can't know what the UI setting is, so
"unnecessary full clone" (`LEAN002`'s premise) isn't something vlotpipe
can honestly assert for Azure. No `LEAN002` analog — this is a real
gap in what static YAML analysis can tell you, not an oversight.

## What ported cleanly (structural, not platform-specific)

`STRUCT001` (no job name suggests test/lint), `STRUCT002` (job has too
many steps), `LEAN008` (duplicate step prefix across jobs), and `LEAN001`
(installing a language runtime via the package manager) all operate
purely on the normalized `Job`/`Step` shape with no GitHub-specific
assumption baked into the *logic* — only their user-facing message
mentioned GitHub-specific tooling (`actions/setup-*`, "composite action
or reusable workflow"). Fixed by branching the message text on
`p.Platform` rather than forking the rule into two near-duplicate
implementations.

`PERF001` (missing dependency cache) also ported, extended to recognize
Azure's `Cache@N` task alongside `actions/cache`/`setup-*`'s `cache:`
input — same underlying "you're re-downloading dependencies every run"
concern, same fix shape (add a cache step), different task name to look
for.

## New Azure-specific rules

**`AZR001`** — missing `timeoutInMinutes`, using the verified 60-minute
Microsoft-hosted default.

**`AZR002`** — a task's `inputs:`/`env:` value that looks like a
credential but isn't a `$(variableName)` reference — the same mistake
`SEC002` catches, expressed in Azure's own variable syntax rather than
GitHub's `${{ secrets.* }}`.

## What's still out of scope for this pass

- **Template resolution.** `- template: path/to/file.yml` (at step,
  job, or stage level) is recorded as an opaque reference, matching how
  the GitHub parser treats a reusable-workflow `uses:` call — the
  referenced file's contents aren't fetched and inlined. Multi-file
  pipeline resolution is a bigger feature than this pass scoped.
- **`resources.repositories`** (checking out additional repos via a
  named resource) isn't parsed.
- **Service connections and variable groups** — Azure's equivalent of
  OIDC/secrets management — aren't modeled. A `SEC012`-shaped
  "dangerous trigger + untrusted input" rule would need Azure's own
  trigger-privilege model researched first (Azure doesn't have a direct
  `pull_request_target` equivalent — YAML pipelines from forks have
  their own, differently-shaped restrictions), which this pass didn't
  cover.
- **Classic (non-YAML) pipelines** aren't in scope at all — vlotpipe
  only parses YAML pipeline definitions, matching its GitHub Actions
  parser's YAML-only scope.

## Detection

`azure-pipelines.yml`/`.yaml` (and the dotfile variants) at any
directory depth — Azure has no fixed folder convention the way GitHub
Actions requires `.github/workflows/`, so this matches on the
conventional filename instead. If a team names their pipeline file
something else entirely (configurable in the Azure DevOps UI), vlotpipe
won't find it yet; pass the exact path to `vlotpipe scan` as a
workaround.
