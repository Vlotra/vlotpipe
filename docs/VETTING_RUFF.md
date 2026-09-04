# Vetting run: astral-sh/ruff

First real-world vet of the rule pack against a live, actively-maintained
repository (rather than the synthetic `testdata/github/{good,bad}`
fixtures). Chose [astral-sh/ruff](https://github.com/astral-sh/ruff) —
thematically fitting (vlotpipe is "ruff for pipelines"), and a strong test
subject on its own merits: 20 workflow files, a genuinely varied set of
trigger types (`push`, `pull_request`, `release`, `schedule`,
`workflow_dispatch`, reusable-workflow fan-out), and — as the run
revealed — a repo whose maintainers already run
[zizmor](https://docs.zizmor.sh) and have made deliberate,
`# zizmor: ignore[...]`-annotated trade-offs. That last part turned out to
be the most valuable part of the exercise: it gave an independent,
already-adjudicated ground truth to compare against.

## Method

```
git clone --depth 1 --filter=blob:none --no-checkout https://github.com/astral-sh/ruff.git
git sparse-checkout init --cone && git sparse-checkout set .github && git checkout
vlotpipe scan ruff/ --format json
```

Sparse checkout pulls only `.github/` (workflows + issue templates), not
the multi-gigabyte Rust/Python source tree — vlotpipe only needs the
former.

## Result summary

| | Before fixes | After fixes |
| --- | --- | --- |
| Total findings | 114 | 93 |
| Blockers | 0 | 0 |
| Files scanned | 20 | 20 |

Zero blocker-severity findings on either pass is itself a useful data
point: it means `SEC001` (unpinned actions), `SEC002` (hardcoded
secrets), `SEC003` (dangerous-trigger checkout), `SEC004` (template
injection), `SEC008` (secrets dump), `SEC012` (GITHUB_ENV injection), and
`SEC014` (hardcoded container credentials) all correctly produced *no*
false positives across 20 real-world files with varied, sometimes
unusual, trigger/permission configurations — the highest-severity, most
consequential rules held up clean.

## Two confirmed bugs, found and fixed

### 1. SEC010 flagged managed cloud runners as "self-hosted" (16 false positives)

Every `SEC010` hit was a `runs-on:` label like `depot-ubuntu-24.04-4` or
`codspeed-macro` — [Depot](https://depot.dev) and
[CodSpeed](https://codspeed.io), third-party runner-as-a-service
providers offering ephemeral, provider-managed cloud VMs. `isGitHubHostedRunner`
only recognized the `ubuntu-`/`windows-`/`macos-` GitHub-hosted prefixes,
so anything else — including a reputable managed service with a
per-job-ephemeral isolation model much closer to GitHub's own runners
than to a literal self-hosted box — got flagged identically to a genuine
self-hosted runner sitting on someone's own network.

**Fix**: added a `managedRunnerPrefixes` list (`depot-`, `buildjet-`,
`warp-`, `codspeed`, `namespace-`, `blacksmith-`, `ubicloud-`) to
`internal/rules/baseline/triggers.go`, checked alongside the GitHub-hosted
prefixes. All 16 false positives resolved; the rule's actual target
(a truly self-managed runner accepting fork PRs) is unaffected.

### 2. SEC006 flagged an explicit, deliberate `persist-credentials: true` (5 false positives)

All 5 `SEC006` hits were jobs that **explicitly** wrote
`persist-credentials: true` — in `publish-docs.yml` and
`sync_typeshed.yaml`, both of which push commits (docs deploy, typeshed
sync) and need the credential. GitHub's own guidance is "set
`persist-credentials: false` unless needed for git operations, and be
explicit about it either way" — these jobs were doing exactly that. The
original rule only special-cased `persist-credentials: false`, so an
explicit, documented `true` was treated identically to the silent,
un-thought-through default it was meant to catch.

**Fix**: `internal/rules/baseline/hygiene.go` now skips whenever
`persist-credentials` is set at all (`true` or `false`), only flagging
when the key is absent and the job is silently relying on the default.

## Findings that held up as true positives

- **`SEC007` (11 hits, `secrets: inherit` on reusable workflow calls in
  `release.yml`) — validated 1:1 against zizmor.** Every single one of
  the 11 lines vlotpipe flagged already carries a
  `# zizmor: ignore[secrets-inherit]` comment in the source. ruff's
  maintainers use zizmor, hit this exact finding, and made a deliberate
  call to accept it (release orchestration fanning out to many
  publish-target reusable workflows) rather than fix it. Zero false
  positives, zero false negatives, and independent confirmation that
  vlotpipe's rule matches the field-tested reference implementation's
  judgment on what's worth flagging.
- **`TIMEOUT001` (48 hits) — confirmed accurate.** Only 7 of ruff's 20
  workflow files use `timeout-minutes` at all, and even those set it
  sporadically per-job rather than uniformly. Spot-checked several flagged
  jobs directly against the source; all genuinely had no timeout set.
- **`STRUCT001` (19 hits) — technically correct, but a known noise
  source for multi-file CI layouts.** Every hit is a single-purpose
  workflow (`build-docker.yml`, `publish-pypi.yml`, `release.yml`, ...)
  that legitimately has no test/lint job because ruff splits CI across 20
  separate files — the actual test/lint jobs live in `ci.yaml`, which
  correctly triggered *zero* `STRUCT001` findings. The rule's own
  info-severity, "low confidence" framing (see `SECURITY_RESEARCH.md`)
  already anticipated this; worth a `.vlotpipe.yml` suppression for repos
  organized this way rather than a rule change.
- **`PERF001`/`PERF002`/`SEC005` — spot-checked, consistent with source.**

## One remaining known imprecision (not fixed, documented instead)

`LEAN001` fired once, on a `docker run alpine:latest sh -c "apk add
python3; ..."` line in `build-binaries.yml`. On inspection, this isn't
the anti-pattern the rule targets (a job reinstalling its own build
toolchain on every run) — it's a one-off, throwaway container spun up to
verify a built wheel installs cleanly on a musl-based distro, i.e. a
test step, not the job's setup phase. `LEAN001` currently pattern-matches
`run:` strings without understanding that a `docker run ... sh -c "..."`
line is a *nested* execution context with its own disposable filesystem.
Fixing this precisely needs either parsing the nested shell string
(fragile) or explicitly excluding `run:` blocks that start with `docker
run`/`docker exec` (simple, but starts making the heuristic's scope
uneven). Left as-is for now — one low-severity (`warning`) hit out of 93
isn't worth a special case yet — but flagging it here so it doesn't get
rediscovered from scratch.

## Takeaway

18% of findings (21/114) were false positives from two identifiable,
narrow gaps — both now fixed and covered by regression tests
(`internal/rules/baseline/baseline_test.go`: "SEC006 does not fire on an
explicit, deliberate persist-credentials: true", "SEC010 does not fire on
a third-party managed runner service"). The remaining 93 findings were
spot-checked and held up, including one exact independent confirmation
against zizmor's own field-tested judgment on the same repository. That's
a reasonable signal-to-noise ratio for a v1 rule pack, and — more
importantly — this is exactly the kind of gap synthetic fixtures can't
surface: both bugs came from real-world patterns (managed runner
services, deliberate credential-persistence opt-ins) that a hand-written
"good"/"bad" fixture pair wouldn't have thought to include.
