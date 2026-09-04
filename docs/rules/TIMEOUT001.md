# TIMEOUT001 — missing-timeout

**Severity:** warning · **Category:** Timeouts · **Autofix:** yes

## What it checks

Flags a job with no `timeout-minutes` set.

## Why it matters

Without an explicit timeout, a job inherits GitHub's default cap of 360
minutes (6 hours). A hung step — a test waiting on a prompt that will
never come, a deadlocked process, a network call retrying forever —
occupies a runner and burns billed minutes for up to six hours before
GitHub finally kills it. A tight `timeout-minutes` turns "stuck for
hours" into "fails fast and loudly."

## Examples

**Flagged**:

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: npm test
```

**Fixed**:

```yaml
jobs:
  test:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - run: npm test
```

Set it a little above the slowest normal run, not the theoretical
maximum — the point is to catch a hang, not to accommodate one.

## Suppressing

The finding is reported on the job's declaration line:

```yaml
  test: # vlotpipe: ignore[TIMEOUT001]
    runs-on: ubuntu-latest
```

Rarely worth suppressing — setting an explicit timeout is close to free
and the failure mode it prevents (silently burning runner-hours) is
expensive.

## Autofix

`vlotpipe scan --fix` (or `check --fix`) inserts `timeout-minutes: 30` —
a modest, explicit placeholder, not a guess at what your job actually
needs. Treat it as a starting point to tune, not a final answer.

Both the inserted value and whether `--fix` touches this code at all
are configurable:

```yaml
# .vlotpipe.yml
rules:
  TIMEOUT001:
    fix_default: 15   # override the inserted value (default: 30)
    fix: false         # or skip the autofix entirely — still fires and reports as usual
```

`fix_default` is independent of `AZR001`'s own — the two can differ.
