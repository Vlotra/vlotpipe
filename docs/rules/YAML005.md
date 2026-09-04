# YAML005 — truthy-value

**Severity:** info · **Category:** YAML · **Platforms:** GitHub Actions, Azure Pipelines

## What it checks

Flags an unquoted mapping *value* that's one of YAML 1.1's boolean-like
spellings beyond `true`/`false`: `yes`/`Yes`/`YES`, `no`/`No`/`NO`,
`on`/`On`/`ON`, `off`/`Off`/`OFF`, `y`/`Y`, `n`/`N`.

Only values are ever checked, never keys — GitHub Actions' own required
`on:` trigger key is never a candidate, by construction, regardless of
config. `true`/`false` themselves (in any casing) are never flagged
either; they're the standard, correct, extremely common way to write a
boolean in CI YAML (`fail-fast: false`, `continue-on-error: true`), and
treating them as suspicious would bury every real finding in noise.

## Why it matters

This is the exact ambiguity real `yamllint` was built to catch: whether
an unquoted `yes`/`no`/`on`/`off` gets read as the literal string or
gets silently coerced to a boolean depends on which YAML parser reads
it. The library this tool is built on resolves booleans per the newer
YAML 1.2 spec (`true`/`false` only) — but that's this one tool. Other
tooling in a pipeline's life doesn't necessarily agree, and the failure
mode when it doesn't is quiet: a value that was meant as a descriptive
string silently becomes `true`, or vice versa.

## Examples

**Flagged** — ambiguous depending on which parser reads it:

```yaml
steps:
  - uses: some/action@v1
    with:
      enabled: yes
```

**Fixed** — quote it if a literal string was intended:

```yaml
steps:
  - uses: some/action@v1
    with:
      enabled: "yes"
```

**Fixed** — or use `true`/`false` if a boolean was actually intended:

```yaml
steps:
  - uses: some/action@v1
    with:
      enabled: true
```

## Suppressing

```yaml
      enabled: yes # vlotpipe: ignore[YAML005]
```

Reasonable when the action's own documented input schema explicitly
expects the string `"yes"`/`"no"` (some do) and quoting it everywhere
just for this rule's sake feels like busywork for a value whose meaning
is unambiguous in that one specific context.
