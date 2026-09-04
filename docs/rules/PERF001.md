# PERF001 — missing-dependency-cache

**Severity:** warning (downgraded to info on unrecognized/self-hosted-looking
runners — see below) · **Category:** Performance

## What it checks

Flags a job that runs a dependency-install command (`npm ci`,
`npm install`, `yarn install`, `pnpm install`, `pip install`,
`poetry install`, `bundle install`, `go mod download`) with no matching
cache step — neither `actions/cache` nor a `setup-*` action's built-in
`cache:` input.

## Why it matters

Without caching, every single run re-downloads and re-resolves the same
dependency tree from scratch, even when the lockfile hasn't changed
since the last run. This is usually the single biggest, cheapest win
available in a slow pipeline.

## Examples

**Flagged**:

```yaml
steps:
  - uses: actions/setup-node@d5df04f8c62c85728eb5c96bb9d0e6ecec1c3d3e # v5.0.1
    with:
      node-version: "22"
  - run: npm ci
```

**Fixed** — most official `setup-*` actions have a built-in `cache:`
input keyed on the lockfile automatically:

```yaml
steps:
  - uses: actions/setup-node@d5df04f8c62c85728eb5c96bb9d0e6ecec1c3d3e # v5.0.1
    with:
      node-version: "22"
      cache: "npm"
  - run: npm ci
```

For ecosystems without built-in caching support, use `actions/cache`
directly, keyed on the lockfile hash:

```yaml
steps:
  - uses: actions/cache@0c907a75c2c80ebcb7f088228285e798b750cf8f
    with:
      path: ~/.cache/pip
      key: pip-${{ hashFiles('requirements.txt') }}
  - run: pip install -r requirements.txt
```

## Self-hosted runners get a lower-confidence version of this finding

The "every run starts from nothing" assumption behind this rule only
holds on an ephemeral runner. If `runs-on:` doesn't look like a
GitHub-hosted label (`ubuntu-*`/`windows-*`/`macos-*`) or a known
managed-ephemeral runner service (Depot, BuildJet, CodSpeed, and
similar — the same list `SEC010` uses), the finding still fires but at
`info` severity instead of `warning`, with a message that says so
explicitly. A persistent self-hosted worker often keeps the previous
job's dependency cache sitting on local disk even with no
`actions/cache` step in the workflow at all — the absence of an
explicit cache step doesn't necessarily mean the absence of a cache. At
the same time, plenty of self-hosted setups (autoscaled via
actions-runner-controller, for example) *are* just as ephemeral as
GitHub's own runners, so this can't be resolved with certainty from the
YAML alone — hence a downgrade, not a suppression.

## Asserting a runner is already cached

If you know for certain a `runs-on:` label is a persistent worker that
already has dependencies cached on disk, say so and get silence instead
of the `info`-severity downgrade above — this is an asserted fact from
the repo owner, not a heuristic, so it suppresses the finding entirely
rather than just lowering its confidence:

```yaml
# .vlotpipe.yml
rules:
  PERF001:
    cached_runners: ["gha-hmak-web", "*"]  # exact label, or "*" for any runner
```

## Suppressing

The finding is reported on the job's declaration line:

```yaml
  build: # vlotpipe: ignore[PERF001]
    runs-on: ubuntu-latest
```

Reasonable for a job that only installs a handful of tiny dependencies
where caching overhead would roughly cancel out the savings, or for a
self-hosted pool you know for certain is persistent (in which case the
`info`-severity finding is already a fair reflection of that).
