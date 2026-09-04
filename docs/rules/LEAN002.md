# LEAN002 — unnecessary-full-clone

**Severity:** info · **Category:** Lean pipelines

## What it checks

Flags an `actions/checkout` step with `fetch-depth: 0` (full git
history) where no other step in the same job appears to need it — no
`describe`, `changelog`, `git log`, `git blame`, or `shortlog` anywhere
in that job's `run:` blocks.

## Why it matters

`actions/checkout` already defaults to `fetch-depth: 1` — just the
commit under test. Explicitly requesting the full history downloads
every commit and blob the repository has ever had, which is slow on any
repo with real age or size, and is usually only worth paying for when a
later step actually walks that history.

## Examples

**Flagged** — nothing in this job looks at git history, but it clones
all of it anyway:

```yaml
steps:
  - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955 # v4.2.2
    with:
      fetch-depth: 0
  - run: npm test
```

**Fixed** — drop the override and use the fast default:

```yaml
steps:
  - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955 # v4.2.2
  - run: npm test
```

Keep `fetch-depth: 0` when it's genuinely needed — for example, a
release job computing a changelog from tag history:

```yaml
steps:
  - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955 # v4.2.2
    with:
      fetch-depth: 0
  - run: git log $(git describe --tags --abbrev=0)..HEAD --oneline
```

## Suppressing

The finding is reported on the checkout step's line:

```yaml
  - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955 # vlotpipe: ignore[LEAN002]
    with:
      fetch-depth: 0
```

The most common legitimate case: history-walking happens inside an
external script or tool this rule can't see into, rather than directly
in a `run:` block in the same job.
