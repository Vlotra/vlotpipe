# YAML003 — trailing-whitespace

**Severity:** info · **Category:** YAML · **Platforms:** GitHub Actions, Azure Pipelines

## What it checks

Flags a line with trailing spaces or tabs before the newline.

## Why it matters

Harmless almost all the time, and exactly the kind of thing a real
`yamllint` run would already be catching if one were wired in — every
diff on that line picks up a whitespace-only change forever after,
purely because something like an editor's trailing-whitespace trim
setting was inconsistent between contributors. Low severity on purpose:
this is hygiene, not a bug.

## Examples

**Flagged** (`·` marking the trailing space that would otherwise be
invisible):

```yaml
jobs:
  build:···
    runs-on: ubuntu-latest
```

**Fixed**:

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
```

## Suppressing

```yaml
  build: # vlotpipe: ignore[YAML003]
```

`vlotpipe format` fixes every occurrence across a file in one pass
(it's a byproduct of the full re-encode — see
[`docs/GETTING_STARTED.md`](../GETTING_STARTED.md#auto-fixing-and-formatting)),
usually a better fit than suppressing one line at a time.
