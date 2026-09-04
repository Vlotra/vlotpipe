# PERF003 — superfluous-action

**Severity:** info · **Category:** Performance

## What it checks

Flags a small set of well-known third-party actions that wrap
functionality already available as a plain CLI command on GitHub-hosted
runners or via a widely-available tool — for example,
`peter-evans/create-pull-request` (wraps `gh pr create`),
`stefanzweifel/git-auto-commit-action` (wraps `git add`/`commit`/`push`),
or `dtolnay/rust-toolchain` (wraps `rustup`).

## Why it matters

Every third-party action in a workflow is a dependency: another
supply-chain surface to keep pinned and updated (see `SUPPLY001`), and
another network fetch on the critical path before the job can even
start doing its actual work. When the same operation is one `gh`/`git`
command away, using the action directly isn't just a security
preference — it's strictly less to maintain and slightly faster every
single run.

## Examples

**Flagged**:

```yaml
steps:
  - uses: peter-evans/create-pull-request@271a8d0340265f705b14b6d32b9829c1cb33d45e # v7.0.5
    with:
      title: "Update generated files"
```

**Fixed**:

```yaml
steps:
  - run: |
      git checkout -b update-generated-files
      git add .
      git commit -m "Update generated files"
      git push origin update-generated-files
      gh pr create --title "Update generated files" --fill
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

## Suppressing

The finding is reported on the step's own line:

```yaml
  - uses: peter-evans/create-pull-request@271a8d0340265f705b14b6d32b9829c1cb33d45e # vlotpipe: ignore[PERF003]
```

Perfectly reasonable to keep the action if it handles edge cases (retry
logic, idempotent PR updates, structured outputs) your own script
doesn't — this is an `info`-severity nudge, not a strong recommendation.
