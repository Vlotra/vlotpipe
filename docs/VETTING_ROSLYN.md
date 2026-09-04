# Vetting run: dotnet/roslyn (Azure Pipelines)

First real-world vet of the new Azure Pipelines support, mirroring the
methodology used for the four GitHub Actions repos (`VETTING_RUFF.md`,
`VETTING_VITE.md`, `VETTING_GIN_VUE_BLOG.md`, `VETTING_AITOS.md`): find
a real, actively-maintained pipeline, run vlotpipe against it, and
verify every finding — and every *absence* of a finding — against the
actual source rather than trusting the tool's own output.

## Picking the repo

Azure Pipelines has no fixed folder convention the way GitHub Actions
does, and there's no equivalent of GitHub's authenticated code-search
API available unauthenticated, so finding a real `azure-pipelines.yml`
took more direct checking than the GitHub runs did. Landed on
[dotnet/roslyn](https://github.com/dotnet/roslyn) — the C# and Visual
Basic compiler, maintained by Microsoft, with a genuine, actively-used
548-line `azure-pipelines.yml` at the repo root. A good stress test
precisely because it's large, mature, and uses advanced Azure YAML
features a smaller/newer pipeline likely wouldn't.

## Method

```
curl -s https://raw.githubusercontent.com/dotnet/roslyn/main/azure-pipelines.yml -o azure-pipelines.yml
vlotpipe scan . -s
```

Downloaded directly rather than sparse-checkout, since it's a single
file at the repo root (no need to clone anything).

## Result: a real parser bug, found and fixed

First pass produced 2 `AZR001` findings — one legitimate
(`Source_Build_Managed`, genuinely missing `timeoutInMinutes`), and one
with an empty job ID: `job '' has no timeoutInMinutes`. That's not a
real job; it's a bug.

**Root cause**: Azure Pipelines supports compile-time conditional
insertion — `${{ if <condition> }}:` (and `${{ each x in y }}:`) as a
mapping key whose value is a nested sequence of items (jobs, stages, or
steps — this syntax is valid anywhere a sequence is). Roslyn's pipeline
uses this at line 247:

```yaml
  - ${{ if ne(variables['Build.Reason'], 'PullRequest') }}:
    - template: eng/pipelines/test-windows-job-single-machine.yml
      parameters: ...
```

The parser was iterating `jobs:`'s sequence content directly, so this
wrapper node — whose only key is the `${{ if ... }}` expression, not
`job:` — got parsed *as if it were a job itself*: no `job:` key means
an empty ID, and none of the real content (the nested `template:`
reference) got looked at, because it's nested one level deeper than the
parser was looking.

**Fix**: `internal/parser/azure/azure.go` gained
`flattenTemplateExpressions`, applied wherever a sequence of
jobs/stages/steps is walked. It detects a single-key mapping node whose
key starts with `${{` and recursively expands its value in place of the
wrapper — treating conditionally-inserted content as unconditionally
present, which is the correct default for static analysis (vlotpipe
can't evaluate `ne(variables['Build.Reason'], 'PullRequest')`, so
"flag what's there" is more useful than "guess whether it runs").
Handles arbitrary nesting (an `${{ if }}` wrapping an `${{ each }}`
wrapping a real job, which does occur in practice — see the second
regression test).

Re-scanned after the fix: exactly one finding, the genuine one.
Confirmed against source directly — grepped the file for `job:` (8
declarations) and `timeoutInMinutes` (7 of the 8 jobs have it set,
matching the single `AZR001` finding on the remaining job) — before
concluding the fix was correct, not just "produces one number now."

## Why this didn't show up in earlier tests

The Azure parser's unit tests (`azure_test.go`) covered stages, jobs,
steps, pool inheritance, `trigger: none`, and opaque template
references — all real Azure features — but none of the hand-written
test fixtures happened to use conditional insertion, since it's a more
advanced feature that a from-scratch example wouldn't naturally
include. This is exactly the value of vetting against a real,
large-scale, actively-maintained pipeline rather than stopping at
synthetic fixtures and unit tests: the gap was invisible until tested
against a file complex enough to actually exercise it.

## Takeaway

Same pattern as every GitHub Actions vetting run: real-world testing
found something synthetic fixtures didn't, the fix is now covered by
regression tests (`TestParseConditionalInsertionIsFlattenedNotTreatedAsAJob`,
`TestParseNestedConditionalInsertionFlattensToRealJobs`), and the final
result — one accurate finding on a 548-line production pipeline, zero
crashes, zero false positives — is a reasonable first real-world
signal that the Azure Pipelines support is solid enough to build on.
