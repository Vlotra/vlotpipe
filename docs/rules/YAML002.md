# YAML002 — duplicate-key

**Severity:** warning · **Category:** YAML · **Platforms:** GitHub Actions, Azure Pipelines

## What it checks

Flags a mapping (a job, a `with:` block, `env:`, anything) that
declares the same key twice.

## Why it matters

YAML allows a duplicate key syntactically — most parsers, including the
one this tool uses, silently keep the *last* value and discard the
first, with no warning. That's an easy, invisible mistake: a
copy-pasted block where the second paste's key never got renamed, a
merge that left both sides' version of the same setting. The file looks
fine and parses fine; it just quietly isn't doing what the first
occurrence implies.

## Examples

**Flagged** — the second `runs-on:` silently wins, the first is dead
text:

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: npm test
    runs-on: windows-latest
```

**Fixed**:

```yaml
jobs:
  build:
    runs-on: windows-latest
    steps:
      - run: npm test
```

## Suppressing

The finding is reported on the *second* (duplicate) key's line — the
message names the line the first occurrence was on, for context:

```yaml
    runs-on: windows-latest # vlotpipe: ignore[YAML002]
```

Hard to imagine a real reason to want this suppressed rather than
fixed — the only value a duplicate key ever "adds" is confusion about
which one actually takes effect.
