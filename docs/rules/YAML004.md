# YAML004 — final-newline

**Severity:** info · **Category:** YAML · **Platforms:** GitHub Actions, Azure Pipelines

## What it checks

Flags a file that doesn't end in exactly one newline — either no
trailing newline at all, or one-or-more extra blank lines piled up at
the end.

## Why it matters

Standard POSIX text-file hygiene, nothing pipeline-specific about it: a
missing final newline shows as a "no newline at end of file" marker in
`git diff` and confuses some line-based tools; extra trailing blank
lines are just clutter that tends to accumulate from repeated editor
saves. Info-severity — this changes nothing about how the pipeline
behaves.

## Examples

**Flagged** — no newline after the last line:

```
    steps:
      - run: echo hi[EOF, no trailing newline]
```

**Flagged** — extra blank lines at the end:

```
    steps:
      - run: echo hi


[EOF]
```

**Fixed**: exactly one newline, nothing after it.

## Suppressing

```yaml
      - run: echo hi # vlotpipe: ignore[YAML004]
```

`vlotpipe format` normalizes this automatically as part of its full
re-encode — see
[`docs/GETTING_STARTED.md`](../GETTING_STARTED.md#auto-fixing-and-formatting).
