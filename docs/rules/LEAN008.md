# LEAN008 — duplicate-step-prefix

**Severity:** info · **Category:** Lean pipelines

## What it checks

Flags a job whose first 3 steps are identical (same action and same
`with:` inputs, or the same `run:` script) to another job's first 3
steps, earlier in the same file.

## Why it matters

The most common form of workflow-file bloat is the same "boilerplate"
opening — checkout, then a language setup action, then a cache step —
copy-pasted into every job because that's the fastest way to get a new
job working. It's also exactly what composite actions and reusable
workflows exist for: say the repeated part once, under a name, and every
job that needs it references that name instead of repeating it.

## Examples

**Flagged** — `build` and `publish` open with the identical three steps:

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955
      - uses: actions/setup-node@d5df04f8c62c85728eb5c96bb9d0e6ecec1c3d3e
        with:
          node-version: "22"
      - run: npm ci
      - run: npm test

  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955
      - uses: actions/setup-node@d5df04f8c62c85728eb5c96bb9d0e6ecec1c3d3e
        with:
          node-version: "22"
      - run: npm ci
      - run: npm publish
```

**Fixed** — the shared setup extracted into a composite action, referenced
by both jobs:

```yaml
# .github/actions/setup/action.yml
runs:
  using: composite
  steps:
    - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955
    - uses: actions/setup-node@d5df04f8c62c85728eb5c96bb9d0e6ecec1c3d3e
      with:
        node-version: "22"
    - run: npm ci
      shell: bash
```

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/setup
      - run: npm test

  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/setup
      - run: npm publish
```

## Suppressing

The finding is reported on the *later* job's declaration line (the one
considered the duplicate, not the first occurrence):

```yaml
  publish: # vlotpipe: ignore[LEAN008]
    runs-on: ubuntu-latest
```

Reasonable when the two jobs are deliberately kept independent for a
reason this rule can't see — for example, one is meant to stay
copy-paste-editable so it can drift from the other without touching a
shared action.
