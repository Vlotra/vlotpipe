# LEAN010 — missing-docker-build-cache

**Severity:** warning (downgraded to info on unrecognized/self-hosted-looking
runners — see below) · **Category:** Lean pipelines

## What it checks

Flags a `docker/build-push-action` step with no `cache-from` and
`cache-to` inputs set.

## Why it matters

Docker's build cache lives on the runner's local disk during the job —
which is thrown away the moment the job ends, since GitHub-hosted
runners are ephemeral. Without an explicit cache backend (most commonly
`type=gha`, which routes through GitHub's own Actions cache, or a
registry-backed cache), every single build starts from zero: every
layer rebuilds from scratch on every run, even if nothing in the
Dockerfile or build context actually changed.

## Examples

**Flagged**:

```yaml
steps:
  - uses: docker/build-push-action@ca877d9245402d1537745e0e356eab639262a132 # v6.15.0
    with:
      push: true
      tags: myapp:latest
```

**Fixed** — export and import the build cache through GitHub's Actions
cache:

```yaml
steps:
  - uses: docker/build-push-action@ca877d9245402d1537745e0e356eab639262a132 # v6.15.0
    with:
      push: true
      tags: myapp:latest
      cache-from: type=gha
      cache-to: type=gha,mode=max
```

## Self-hosted runners get a lower-confidence version of this finding

Like `PERF001`, the "every build starts from zero" framing assumes the
runner itself is thrown away between jobs. If the job's `runs-on:`
doesn't look GitHub-hosted or like a known managed-ephemeral runner
service, this still fires but at `info` instead of `warning` — Docker's
local build cache may well already be sitting on disk from the previous
job on a persistent self-hosted worker, even with no `cache-from`/
`cache-to` configured at all.

## Suppressing

The finding is reported on the step's line:

```yaml
  - uses: docker/build-push-action@ca877d9245402d1537745e0e356eab639262a132 # vlotpipe: ignore[LEAN010]
    with:
      push: true
```

Reasonable for a genuinely one-off or from-scratch-by-design build (a
nightly full-rebuild job meant to catch base-image drift, for example),
where reusing a cache would defeat the point — or for a self-hosted pool
known for certain to be persistent, where the `info`-severity finding
already reflects that.
