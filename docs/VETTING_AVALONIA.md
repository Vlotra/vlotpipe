# Vetting run: AvaloniaUI/Avalonia (Azure Pipelines)

Second real-world vet of the Azure Pipelines support, same methodology as
`VETTING_ROSLYN.md`: find a real, popular, actively-maintained pipeline,
run vlotpipe against it, and verify every finding — and every *absence*
of a finding — against the actual source rather than trusting the tool.

This run was deliberately picking for a *messier* result than Roslyn's:
Roslyn's `azure-pipelines.yml` turned out to be a large but disciplined
file (7 of 8 jobs already had `timeoutInMinutes` set). The goal here was
a repo where vlotpipe's findings tell a real "this needs attention"
story, not just a single missed edge case.

## Picking the repo

[AvaloniaUI/Avalonia](https://github.com/AvaloniaUI/Avalonia) — the
leading open-source, cross-platform .NET UI framework (the closest thing
the .NET world has to a WPF/MAUI alternative that also runs on Linux and
macOS; it's what JetBrains Rider's own UI is partly built on). At the
time of this run: **31,440 stars**, and pushed **the same day** this vet
was run — about as "real and currently maintained" as an open-source
repo gets.

## Method

```
curl -s https://raw.githubusercontent.com/AvaloniaUI/Avalonia/main/azure-pipelines.yml -o azure-pipelines.yml
vlotpipe scan . -s
vlotpipe scan .
```

## Result

```
1 file scanned, 5 violations found

By severity
  blocker   0
  warning   4
  info      1

By rule
  ● AZR001      4
  ● STRUCT001   1

azure-pipelines.yml:3:3:   AZR001 job 'GetPRNumber' has no timeoutInMinutes; defaults to 60 minutes on Microsoft-hosted agents (unlimited on self-hosted)
azure-pipelines.yml:3:3:   STRUCT001 no job name suggests a test/lint/check step runs in this pipeline
azure-pipelines.yml:24:3:  AZR001 job 'Linux' has no timeoutInMinutes; defaults to 60 minutes on Microsoft-hosted agents (unlimited on self-hosted)
azure-pipelines.yml:61:3:  AZR001 job 'macOS' has no timeoutInMinutes; defaults to 60 minutes on Microsoft-hosted agents (unlimited on self-hosted)
azure-pipelines.yml:141:3: AZR001 job 'Windows' has no timeoutInMinutes; defaults to 60 minutes on Microsoft-hosted agents (unlimited on self-hosted)
```

**Every single job in the file — all four of them — is missing
`timeoutInMinutes`.** This isn't a partial oversight on one job; it's
the pipeline's default posture. The four jobs are a real build matrix:
`GetPRNumber` (lightweight), `Linux` (installs Android/macOS/wasm
workloads, full build + test run), `macOS` (installs mobile workloads,
runs an Xcode build, generates native headers, builds, publishes three
artifact sets), and `Windows` (installs workloads, builds via Nuke,
publishes three artifact sets). The `macOS` and `Windows` jobs in
particular chain several non-trivial, non-deterministic steps
(workload installation over the network, native toolchain builds) — the
kind of job that occasionally hangs rather than fails cleanly, which is
exactly the scenario a timeout bound exists for. Without one, a stuck
step silently eats the full 60-minute Microsoft-hosted default before
Azure kills it — no earlier signal, no faster feedback.

## Independent verification

Trusting the tool's own count is not verification — confirmed directly
against source before writing this up:

```
$ grep -c "^- job:" azure-pipelines.yml
4
$ grep -n "^- job:" azure-pipelines.yml
3:- job: GetPRNumber
24:- job: Linux
61:- job: macOS
141:- job: Windows
$ grep -n "timeoutInMinutes" azure-pipelines.yml
(no output)
```

Four job declarations, zero `timeoutInMinutes` occurrences anywhere in
the 189-line file. Matches vlotpipe's four `AZR001` findings exactly —
not a parser miscount, not a false positive.

`STRUCT001` (info: no job name suggests a test/lint/check step runs)
fired once, on `GetPRNumber` specifically — worth noting this is a
*low-confidence* heuristic by design (it only looks at job names, not
step contents), and it's correctly quiet on `Linux` and `macOS` even
though neither job is *named* "test": both contain a
`PublishTestResults@2` step, and the rule's job-name-keyword check
alone isn't what's firing here. No false positive.

No `AZR002` (hardcoded credential) findings — checked manually, and
correctly so: every value in the file is either a literal build
parameter (versions, target names) or a `$(...)` pipeline-variable
reference; there's no embedded secret to catch.

## A second finding, and a second bug

Two of the jobs (`Linux`, `macOS`) run `printenv` as part of a debug
step (`dotnet --info` / `printenv` before the real build command) —
dumping the full CI environment, including whatever secrets Azure
injects as env vars, straight into the build log. At the time this was
first noticed, vlotpipe had no rule for it — same class of problem as
`SEC008` (`toJSON(secrets)` dumping the entire GitHub Actions secrets
context) but expressed as a plain shell command instead of an
expression, so it needed its own detection, not a platform-specific
variant of an existing one.

Built `SEC016` (`environment-dump`, warning, dual-platform — see
[`docs/rules/SEC016.md`](rules/SEC016.md)) to catch bare `printenv`/
`env`/cmd.exe's `set`/PowerShell's `Get-ChildItem Env:`, deliberately
excluding the safe single-variable forms (`printenv HOME`, `env
FOO=bar some-command`).

First run against this exact file found **zero** hits — a real bug, not
a clean result. Avalonia's `printenv` steps are written as
`task: CmdLine@2` with the script in `inputs.script`, not the
`script:`/`bash:` shorthand that puts it in `step.Run`. `SEC016`'s first
cut only checked `step.Run`, which is exactly right for GitHub Actions'
`run:` and Azure's shorthand steps, but silently misses the *far* more
common Azure pattern of a task carrying its command in
`inputs.script`/`inputs.inlineScript` — which is how Avalonia, and every
other Azure pipeline vetted so far in this project (Newtonsoft.Json's
`AzureCLI@2`, sonar-dotnet's `CmdLine@2`), actually write scripts.

First patched at the rule level (`SEC016` scanning `step.With["script"]`/
`step.With["inlineScript"]` alongside `step.Run`), then pulled upstream
into the parser instead: `internal/parser/azure/azure.go`'s `task:` case
now also populates `step.Run` itself whenever `inputs.script` or
`inputs.inlineScript` is present, the same value `step.With` already
carries. That's the more correct fix — the gap wasn't specific to
`SEC016`, it was in how the parser exposed task-based script content at
all. `PERF001` and `LEAN001` are both dual-platform and both keyed
entirely off `step.Run` to detect dependency-install commands
(`installsDeps` in `perf.go`, the package-manager regex in `lean.go`) —
before this fix, *both* had the identical blind spot on any Azure
pipeline that installs dependencies via `task: CmdLine@2`/`Bash@3`
rather than the `script:`/`bash:` shorthand, silently missing
`npm install`, `pip install`, and the rest of `installCommands` the same
way `SEC016` missed `printenv`. None of the four real Azure pipelines
vetted so far happen to trigger that specific combination, so it wasn't
caught by a live finding — it surfaced by reasoning about what else
reads `step.Run` once the first bug was found, not by observing a second
wrong answer. Re-ran all four real files after the parser fix: identical
results everywhere except Avalonia's two new `SEC016` hits, confirming
no unrelated rule started firing on content it shouldn't. `SEC016`
itself simplified back down to checking only `step.Run`, now that the
parser guarantees it's there. Covered by
`TestEnvironmentDumpFiresOnAzureTaskInlineScript` in
`internal/rules/baseline/baseline_test.go` (the rule-level regression)
and `TestParseTaskInlineScriptPopulatesRun` in
`internal/parser/azure/azure_test.go` (the parser-level one).

## Takeaway

A 31k-star, actively-maintained-today project, built partly by the same
team that ships JetBrains Rider's UI, has no timeout governance at all
across its entire CI matrix. That's not a contrived example — it's the
real, current state of a pipeline a lot of people depend on. `vlotpipe
scan` surfaces it in under a second with no configuration. Combined with
Roslyn's largely-clean result, these two runs show the range: sometimes
the tool finds one real gap in an otherwise disciplined pipeline,
sometimes it finds that a whole dimension of policy was never
established at all.

The final tally on this file, after `SEC016` and its own bug fix, is 7
violations (4× `AZR001`, 2× `SEC016`, 1× `STRUCT001`) — up from the 5 in
the initial run. Same pattern as every prior vetting run in this
project: real-world testing against a large, actively-maintained
pipeline finds gaps synthetic fixtures don't, in the tool's rule
coverage this time rather than its parser.
