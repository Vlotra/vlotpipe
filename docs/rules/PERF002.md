# PERF002 — missing-concurrency-group

**Severity:** info · **Category:** Performance · **Autofix:** yes

## What it checks

Flags a workflow triggered by `pull_request` with no top-level
`concurrency:` block.

## Why it matters

Without a concurrency group, pushing a second commit to the same PR
doesn't cancel the still-running check from the first push — both keep
occupying runners and billed minutes until they finish, even though only
the newer one's result will ever matter.

## Examples

**Flagged**:

```yaml
name: CI
on: pull_request
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: npm test
```

**Fixed**:

```yaml
name: CI
on: pull_request
concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: npm test
```

## Suppressing

This finding is always reported at line 1 of the file (it's a
whole-workflow property, not tied to any specific job or step), so the
inline comment has to go on the file's very first line:

```yaml
name: CI # vlotpipe: ignore[PERF002]
on: pull_request
```

Using `.vlotpipe.yml` instead is usually cleaner for this one, since
"line 1" isn't a natural place to explain the reasoning:

```yaml
ignore:
  - code: PERF002
    path: ci.yml
    reason: "single short job, cancellation overhead isn't worth it"
```

## Autofix

`vlotpipe scan --fix` (or `check --fix`) inserts the exact block shown
above (`group: ${{ github.workflow }}-${{ github.ref }}`,
`cancel-in-progress: true`) immediately before `jobs:` — the standard
form for this pattern, not something specific to your workflow, so
review it if a workflow has a reason to keep superseded runs alive.
