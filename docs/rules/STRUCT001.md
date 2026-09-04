# STRUCT001 — missing-test-job

**Severity:** info · **Category:** Structure

## What it checks

A whole-file, low-confidence heuristic: if *no* job's ID or name
contains "test", "lint", "check", "verify", or "ci" (case-insensitive),
it fires once, on the first job's line.

## Why it matters

A CI workflow with nothing that looks like a test, lint, or check step
might just be a build/publish/deploy workflow that's missing quality
gates entirely — or it might be a repo that (correctly) splits concerns
across multiple workflow files, where the actual tests live somewhere
else. This rule can't tell the difference from one file alone, which is
exactly why it's `info` severity rather than a stronger signal.

## Examples

**Flagged** — a workflow that only builds and pushes a Docker image,
with nothing that looks like a quality gate:

```yaml
name: Build and Push
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: docker build -t myapp .
      - run: docker push myapp
```

**Fixed** — either add a job whose name signals what it does:

```yaml
name: Build and Push
on: push
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: npm test
  build:
    needs: test
    runs-on: ubuntu-latest
    steps:
      - run: docker build -t myapp .
      - run: docker push myapp
```

...or, if tests genuinely live in a separate workflow file by design
(a common, reasonable pattern in repos that split CI across many
single-purpose files), suppress it there instead of restructuring.

## Suppressing

The finding is reported on the first job's declaration line — since
it's a whole-file property, `.vlotpipe.yml` is usually the better fit:

```yaml
ignore:
  - code: STRUCT001
    path: "build-and-push.yml"
    reason: "tests run in ci.yml; this file only builds and publishes"
```

Or inline, on the first job's line, if you'd rather keep the reasoning
in the workflow file itself:

```yaml
jobs:
  build: # vlotpipe: ignore[STRUCT001]
    runs-on: ubuntu-latest
```
