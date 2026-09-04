# LEAN011 — missing-artifact-retention

**Severity:** info · **Category:** Lean pipelines · **Autofix:** yes

## What it checks

Flags an `actions/upload-artifact` step with no `retention-days` input
set.

## Why it matters

`actions/upload-artifact` defaults to keeping the uploaded artifact for
90 days. That's frequently far longer than the artifact is actually
useful for — a test-run's log output, a coverage report, or an
intermediate build product only ever consumed by a later job in the
*same* run has no reason to sit in storage for three months. It's not a
speed issue like most of this category, but it's real, quietly
accumulating cost for artifacts nobody will ever download again.

## Examples

**Flagged**:

```yaml
steps:
  - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02 # v4.6.2
    with:
      name: test-logs
      path: test-results/
```

**Fixed**:

```yaml
steps:
  - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02 # v4.6.2
    with:
      name: test-logs
      path: test-results/
      retention-days: 3
```

Artifacts meant to actually be downloaded later — a release build, a
compliance record — may genuinely want the long default, or something
even longer via the org-level retention setting.

## Suppressing

The finding is reported on the step's line:

```yaml
  - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02 # vlotpipe: ignore[LEAN011]
    with:
      name: release-binary
```

Expected for artifacts that are the actual point of the workflow (a
built release asset) rather than CI-run-scoped debugging output.

## Autofix

`vlotpipe scan --fix` (or `check --fix`) adds `retention-days: 7` —
deliberately not `90` (that's the existing default; writing it back
would silence the finding without changing anything real), and shorter
than the `3` in the example above just to keep the placeholder generic.
Creates the step's `with:` block if it doesn't have one yet; skipped if
an existing `with:` block is flow-style.
