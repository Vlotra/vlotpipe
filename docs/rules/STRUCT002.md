# STRUCT002 — job-too-many-steps

**Severity:** info · **Category:** Structure

## What it checks

Flags a job with more than 20 steps (configurable — see below).

## Why it matters

Jobs rarely get this large on purpose; they accrete, one step at a time,
PR after PR, until reviewing or modifying them means scrolling through
dozens of near-identical blocks. Past a certain size, a composite action
or reusable workflow says the repeated parts once, under a name, instead
of spelling them out inline every time.

The default threshold (20) isn't arbitrary — it's calibrated against
real workflows vetted while building vlotpipe (see `docs/VETTING_*.md`):
even astral-sh/ruff's most complex CI job tops out at 15 steps, so 20
catches genuine bloat without flagging legitimately large, well-run
jobs.

## Examples

**Flagged** — a job that's grown past the point of being easy to scan:

```yaml
jobs:
  release:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955
      # ... 24 more steps: build, sign, notarize, package for 4 platforms,
      # upload to 3 registries, notify 2 Slack channels ...
```

**Fixed** — the platform-specific parts extracted into a reusable
workflow, called once per platform instead of inlined 4 times over:

```yaml
jobs:
  release:
    strategy:
      matrix:
        platform: [linux, macos, windows]
    uses: ./.github/workflows/build-and-sign.yml
    with:
      platform: ${{ matrix.platform }}
```

## Configuring the threshold

```yaml
# .vlotpipe.yml
max_steps_per_job: 30
```

Unlike most rules, this one has no fixed "right" number — different
teams have genuinely different appetites for job size. The default is a
starting point, not a mandate.

## Suppressing

The finding is reported on the job's declaration line:

```yaml
  release: # vlotpipe: ignore[STRUCT002]
    runs-on: ubuntu-latest
```

Reasonable for a job that's long because it's doing one genuinely long
linear sequence (a multi-stage release process) rather than repeating
itself — this rule can't tell the difference between "long and
repetitive" and "long and irreducibly sequential."
