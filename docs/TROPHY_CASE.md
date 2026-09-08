# Trophy case

Real bugs, found by running vlotpipe against real, actively-maintained
pipelines — not synthetic `testdata/` fixtures — and verified against
the actual source before being called a bug at all. Every fix below
shipped with a regression test named in this doc, so the specific
real-world shape that broke it can't silently regress.

See [`docs/VETTING_*.md`](.) for the full write-up of each run this
came from — method, every finding spot-checked, what held up as a true
positive.

## Managed cloud runners flagged as self-hosted (`SEC010`)

**Found vetting:** [astral-sh/ruff](https://github.com/astral-sh/ruff)
**16 false positives, one run.**

Every `SEC010` hit was a `runs-on:` label like `depot-ubuntu-24.04-4` or
`codspeed-macro` — third-party runner-as-a-service providers
([Depot](https://depot.dev), [CodSpeed](https://codspeed.io)) offering
ephemeral, provider-managed cloud VMs. The rule only recognized GitHub's
own `ubuntu-`/`windows-`/`macos-` hosted prefixes, so a reputable managed
service with an isolation model much closer to GitHub-hosted than to a
genuine self-hosted box got flagged identically to one.

**Fix:** a `managedRunnerPrefixes` list (`depot-`, `buildjet-`, `warp-`,
`codspeed`, `namespace-`, `blacksmith-`, `ubicloud-`) in
`internal/rules/baseline/triggers.go`. Regression test: *"SEC010 does not
fire on a third-party managed runner service."*

## An explicit, deliberate `persist-credentials: true` flagged as a mistake (`SEC006`)

**Found vetting:** [astral-sh/ruff](https://github.com/astral-sh/ruff)
**5 false positives.**

All 5 hits were jobs that **explicitly** wrote `persist-credentials:
true` — deliberate opt-ins for jobs that push commits (a docs deploy, a
typeshed sync). The rule only special-cased the `false` value; an
explicit, documented `true` was treated identically to the silent
default it exists to catch.

**Fix:** `internal/rules/baseline/hygiene.go` now skips whenever
`persist-credentials` is set at all, either value — only flags when the
key is absent. Regression test: *"SEC006 does not fire on an explicit,
deliberate persist-credentials: true."*

## `issue_comment` missing from the dangerous-trigger list (`SEC003`)

**Found vetting:** [vitejs/vite](https://github.com/vitejs/vite)

vite's bot automation includes a ChatOps-style workflow triggered by
`issue_comment` that checks out a PR head based on the comment — exactly
the privilege-escalation shape `SEC003` exists to catch (an
attacker-invokable trigger with real `GITHUB_TOKEN` scope, checking out
untrusted code) — but `issue_comment` wasn't in the rule's trigger list
at all, so it silently passed.

**Fix:** added `issue_comment` to the dangerous-trigger set in
`internal/rules/baseline/triggers.go`.

## Nested conditional insertion not flattened to real jobs (Azure parser)

**Found vetting:** [dotnet/roslyn](https://github.com/dotnet/roslyn)
(Azure Pipelines, 548 lines)

Azure YAML's `${{ if }}:` conditional-insertion syntax can nest, and
jobs declared inside a nested conditional block weren't being flattened
into the parsed job list at all — they silently vanished from every
rule's view, rather than erroring or producing a wrong finding.

**Fix:** `internal/parser/azure/azure.go`'s conditional-insertion
handling now recurses. Regression tests:
`TestParseConditionalInsertionIsFlattenedNotTreatedAsAJob`,
`TestParseNestedConditionalInsertionFlattensToRealJobs`.

## Azure `task:`-based scripts invisible to every `step.Run`-keyed rule (`SEC016` + parser)

**Found vetting:** [AvaloniaUI/Avalonia](https://github.com/AvaloniaUI/Avalonia)
(Azure Pipelines, 31k+ stars)

Two jobs ran `printenv` — dumping the full CI environment, including
whatever secrets Azure injects as env vars, into the build log. First
run against this exact file found **zero** hits, which turned out to be
the actual bug: Avalonia writes these as `task: CmdLine@2` with the
script in `inputs.script`, not the `script:`/`bash:` shorthand that
populates `step.Run` — the form the brand-new `SEC016` rule (built for
exactly this class of finding, see below) checked.

The deeper fix wasn't at the rule level: `PERF001` and `LEAN001` are
both dual-platform and both keyed entirely off `step.Run` to detect
dependency-install commands, so *both* had the identical blind spot on
any Azure pipeline using `task: CmdLine@2`/`Bash@3` instead of the
shorthand — a second bug found by reasoning about what else reads
`step.Run`, not by a second wrong answer observed live.

**Fix:** pulled upstream into the parser —
`internal/parser/azure/azure.go`'s `task:` case now populates
`step.Run` itself whenever `inputs.script`/`inputs.inlineScript` is
present, so every rule keyed off `step.Run` gets task-based scripts for
free, with no per-rule special case. Regression tests:
`TestEnvironmentDumpFiresOnAzureTaskInlineScript`,
`TestParseTaskInlineScriptPopulatesRun`.

## A rule born from a real gap, not a hypothetical (`SEC016`)

**Found vetting:** [AvaloniaUI/Avalonia](https://github.com/AvaloniaUI/Avalonia)

Before this run, vlotpipe had no rule for a bare `printenv`/`env`
dumping the whole CI environment (including injected secrets) to the
build log — the same class of problem as `SEC008`
(`toJSON(secrets)`), but expressed as a plain shell command instead of
a GitHub Actions expression, so it needed its own detection. Built
directly from this finding: [`SEC016`](rules/SEC016.md)
(`environment-dump`), dual-platform, deliberately excluding safe
single-variable forms (`printenv HOME`, `env FOO=bar some-command`).

## Independent validation, not a bug — `SEC007` matched zizmor 1:1

**Found vetting:** [astral-sh/ruff](https://github.com/astral-sh/ruff)

Every one of 11 `SEC007` hits (`secrets: inherit` on reusable workflow
calls in `release.yml`) already carries a `# zizmor: ignore[secrets-inherit]`
comment in ruff's own source — ruff's maintainers run
[zizmor](https://docs.zizmor.sh), hit this exact finding, and made a
deliberate call to accept it (release orchestration fanning out to many
publish targets). Zero false positives, zero false negatives, and
independent confirmation that vlotpipe's rule matches a field-tested
reference implementation's judgment on the same real repository — not a
bug fixed, but the strongest kind of evidence a rule is calibrated
right.
