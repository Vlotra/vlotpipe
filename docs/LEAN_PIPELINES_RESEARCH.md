# Lean Pipelines Research — build-time & pipeline-size rule category

Follow-up to `SECURITY_RESEARCH.md`, same purpose: ground a new vlotpipe
rule category in real guidance before writing code. Question this time
isn't "is this workflow safe" but "is this workflow wasteful" — short
pipeline *files* (DRY, no copy-pasted step blocks) and short pipeline
*runtimes* (nothing reinstalled, redownloaded, or rerun that didn't need
to be). Sources: [StarSling's GitHub Actions best-practices
catalog](https://starsling.dev/best-practices/github-actions) (18
practices across caching/parallelization/trigger-scope/runner-queue —
the closest existing "rule catalog" for speed, same role zizmor played
for the security research), GitLab's [Pipeline
efficiency](https://docs.gitlab.com/ci/pipelines/pipeline_efficiency/)
docs, Docker's [Building best
practices](https://docs.docker.com/build/building/best-practices/), and
GitHub's own checkout/caching docs.

## The core insight

Every source converges on the same ordering principle: **cut the work
before you speed up the work.** Concretely, in priority order:

1. Don't run the job at all if the change can't affect its outcome (path
   filters, changed-file scoping).
2. Don't run a superseded job (`concurrency` + `cancel-in-progress`).
3. Don't fetch/build/install more than the job needs (shallow checkout,
   prebuilt images instead of per-run installs, `.dockerignore`).
4. Only then optimize what's left (caching, sharding, right-sized
   runners).

vlotpipe's existing `PERF001` (missing dependency cache) already covers
step 4 partially. Everything below is largely steps 1–3, which is where
the biggest wins tend to live — a docs-only PR that still spins up a full
matrix, or a job that reinstalls a compiler toolchain via `apt-get` on
every single run, wastes far more time than an uncached `npm ci` does.

## Rule category taxonomy

### LEAN — build-time & pipeline-size (new category)

The user's example — "installing python to ubuntu... instead of an
image" — is the canonical case: a `runs-on: ubuntu-latest` job that
`apt-get install`s a language runtime or compiler toolchain from scratch
every run, when either (a) a maintained `setup-*` action with built-in
caching exists, or (b) the job should run inside a container image that
already has the toolchain baked in.

| Code | Severity | Checks | Source |
| --- | --- | --- | --- |
| LEAN001 ✅ implemented | warning | job runs `apt-get install`/`apk add`/manual curl-and-untar to install a language runtime or compiler (python, node, go, java, rust, ruby, etc.) instead of using the corresponding `actions/setup-*` action or a `container:` image that already has it | user's own example; StarSling caching category; GitLab "pre-installed software" guidance |
| LEAN002 ✅ implemented | info | `actions/checkout` step explicitly sets `fetch-depth: 0` (full history) with no later step that plausibly needs it (`git describe`, `git log`, changelog generation, blame) | StarSling `shallow-checkout` — default is already `fetch-depth: 1`; the anti-pattern is *opting into* full history unnecessarily |
| LEAN003 | info | workflow triggers on `push`/`pull_request` with no `paths`/`paths-ignore` filter, in a repo that looks like a monorepo (multiple top-level dirs with independent manifests: multiple `package.json`/`go.mod`/`pom.xml`) | StarSling `path-filter-workflows`; GitLab "reduce how often jobs run"; real complaint thread (github/orgs/community#177835) |
| LEAN004 | warning | no `concurrency:` group with `cancel-in-progress: true` on a workflow triggered by `pull_request` — superseded pushes keep old runs occupying runners | StarSling `cancel-superseded-runs`; zizmor `concurrency-limits` (already listed under `PERF002` in the security doc — same underlying check, cross-reference rather than duplicate) |
| LEAN005 | info | `Dockerfile`/inline `docker build` step has a single `FROM` with build tools (compilers, `-dev` headers, package-manager caches) present in the same stage that's later pushed/run, instead of a multi-stage build that copies only the built artifact into a slim final stage | Docker Building Best Practices (multi-stage builds); GitLab "Optimize Docker images" |
| LEAN006 | info | `RUN apt-get install` without `--no-install-recommends` and without removing `/var/lib/apt/lists/*` in the *same* `RUN` layer | Docker Building Best Practices (`apt-get` section); GitLab Docker-image checklist |
| LEAN007 | info | base image in `FROM` or `container:` is a full OS image (`ubuntu`, `debian`) where a slim/alpine/distroless variant of the same official image exists and no build-only tooling in later steps requires the full OS | GitLab "reduce Docker image size"; Docker docs (Alpine recommendation) |
| LEAN008 ✅ implemented | info | two or more jobs in the same workflow file repeat an identical *prefix* of steps instead of factoring it into a reusable workflow or composite action | ties back to "pipeline files can be short" — DRY at the workflow-authoring level, not just runtime; GitHub reusable-workflows docs |
| LEAN009 | info | matrix `strategy` has a high fan-out (e.g. >6 combinations) on a job with no corresponding test-sharding rationale (single test command duplicated per cell rather than partitioned) | StarSling `shard-tests`; matrix-build guides — a large matrix multiplies *setup* cost (checkout+install×N) even when the actual test time per cell is small |

### PERF — cross-reference (already-scoped items, no duplication)

Two LEAN candidates turned out to already be scoped under `PERF` in the
prior security research pass — recorded there, not duplicated here:

- **`PERF002`** (concurrency/cancel-in-progress) — same underlying check
  as `LEAN004` above; keeping it under `PERF` since it's purely a
  workflow-level speed knob, not a "the pipeline file/image is bloated"
  concern.
- **`PERF003`** (superfluous actions duplicating preinstalled runner
  tools) — directly relevant to "lean," since replacing e.g.
  `dtolnay/rust-toolchain` with plain `rustup` both shortens the pipeline
  file *and* removes a third-party action fetch from the critical path.

`PERF001` (missing dependency cache) should also gain a stricter variant
once implemented further: a cache key that doesn't include
`hashFiles(<lockfile>)` causes silent full cache misses — same
underlying rule, worth a note for whoever implements `PERF001` v2 rather
than a new code.

### STRUCT — cross-reference

No new STRUCT items emerged from this pass; `STRUCT001` (missing
test/lint job) is about pipeline *completeness*, not brevity, so it
stays separate from `LEAN`.

## Concrete example matching the user's prompt

The "install python to ubuntu, one dependency at a time" anti-pattern,
side by side with what `LEAN001` should flag/suggest:

```yaml
# flagged by LEAN001
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: sudo apt-get update && sudo apt-get install -y python3.12 python3-pip
      - run: pip install -r requirements.txt
```

```yaml
# clean — cached setup action, no apt-get at all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/setup-python@0b93645e9fea7318ecaed2b359559ac225c90a2b # v5.3.0
        with:
          python-version: "3.12"
          cache: "pip"
      - run: pip install -r requirements.txt
```

```yaml
# also clean — toolchain baked into the image, nothing installed at CI time
jobs:
  build:
    runs-on: ubuntu-latest
    container: python:3.12-slim
    steps:
      - run: pip install -r requirements.txt
```

`LEAN001`'s detection is a `run:` string match against a small table of
package-manager install commands (`apt-get install`, `apk add`,
`yum install`) combined with a small table of language-runtime package
names (`python3`, `nodejs`, `golang`, `openjdk`, `ruby`), scoped to jobs
that don't already declare a matching `container:` image — same shape as
the existing `PERF001` heuristic (`internal/rules/baseline/perf.go`), so
it's a same-day addition once prioritized.

## Suggested priority

1. **LEAN001** — directly requested, cheap YAML-pattern check, same shape
   as existing `PERF001`.
2. **LEAN004** (or its `PERF002` twin) — single highest-leverage fix per
   every source surveyed; also cheap to detect (presence/absence of a
   top-level `concurrency:` key).
3. **LEAN003** — highest real-world payoff in monorepos, but the
   "looks like a monorepo" heuristic needs care to avoid false positives
   on small repos; scope after LEAN001/004 land and can be validated
   against real fixtures.
4. **LEAN005–007** (Dockerfile-focused) — depend on vlotpipe eventually
   parsing `Dockerfile`/`docker build` steps at all, which today it
   doesn't; worth flagging as a parser-scope decision rather than
   something to bolt onto the GitHub Actions YAML parser.
5. **LEAN008** ✅ implemented (round 3, below). **LEAN009** remains
   deferred — still structural/heuristic rather than pattern-matchable,
   closer to STRUCT001's "info, low confidence" style than a hard
   blocker/warning.

## Round 3: pipeline tidiness / bloat control

A distinct angle from "fast" and "secure": does the pipeline *file
itself* stay easy to read and modify as a team keeps adding to it over
time. Two rules came out of this round, both `info` severity — nudges,
not strong assertions, matching the confidence level of a heuristic that
can't fully distinguish "genuinely repetitive" from "coincidentally
similar."

**`LEAN008` — duplicate step prefix.** ✅ implemented. Originally scoped
in round 1 with a "≥4 identical steps" threshold; shipped with 3 instead,
calibrated against what actually turned up scanning real repos rather
than a guessed number. Detects two or more jobs in the same file whose
first 3 steps are byte-identical (same `uses:`+`with:`, or same `run:`
script) — the standard "checkout, setup, cache, then diverge" pattern.
Verified against astral-sh/ruff's `build-binaries.yml`: `sdist` and
`linux-cross` (two jobs building for different target platforms) share
an identical `actions/checkout` + `actions/setup-python` opening,
confirming this is a real, not just theoretical, pattern in
well-maintained multi-platform build files.

**`STRUCT002` — job has too many steps.** ✅ implemented, new (not in the
original round-1 or round-2 candidate lists — this one came directly
from a "how do you keep pipelines from growing unbounded" framing, a
different question from either "is it fast" or "is it safe"). Flags a
job whose step count exceeds a threshold, defaulting to 20 but
overridable via `.vlotpipe.yml`'s `max_steps_per_job:` — deliberately
configurable rather than fixed, since "how big is too big" is a genuine
team-by-team judgment call in a way that, say, "should you cache
dependencies" isn't. The default of 20 was calibrated the same way as
`LEAN008`'s threshold: checked against every job across all four vetted
repos, and the single largest job found (ruff's `ecosystem`/`test` jobs,
15 steps) sets the ceiling for "this is normal" before 20 kicks in.

## Round 2: deeper research — what else is out there

First pass covered the workflow-level knobs (caching, concurrency,
checkout depth) and stopped at the Dockerfile boundary. This pass went
looking specifically for what's *below* that — compiler-level and
build-tool-level caching — and at two things that turned out to be
real but explicitly *not* lintable from YAML alone, which is worth
documenting so they don't get rediscovered as "why doesn't vlotpipe
check this."

### Implemented this round

**`LEAN010` — missing Docker build cache backend.** ✅ implemented.
`docker/build-push-action` (the standard action wrapping Buildx) has no
persistent cache by default — the runner's local Docker build cache
lives on ephemeral disk that's gone the moment the job ends, so without
an explicit `cache-from`/`cache-to` pair (most commonly `type=gha` to
route through GitHub's own Actions cache, or a registry-backed cache),
every image rebuild re-runs every layer from scratch. Unlike the
Dockerfile-internal concerns (`LEAN005`–`007`), this is checkable
without parsing a Dockerfile at all — `cache-from`/`cache-to` are inputs
on the GitHub Actions step itself. Source: [Docker's own GitHub Actions
cache backend docs](https://docs.docker.com/build/cache/backends/gha/).

**`LEAN011` — missing artifact retention-days.** ✅ implemented, `info`
severity (a storage-cost nudge, not a speed concern).
`actions/upload-artifact` defaults to 90-day retention if
`retention-days` isn't set, which is frequently far longer than the
artifact is useful for (test logs, coverage reports, anything consumed
by a later job in the same run). Source: [GitHub's own retention-period
docs](https://docs.github.com/en/organizations/managing-organization-settings/configuring-the-retention-period-for-github-actions-artifacts-and-logs-in-your-organization)
confirm the 90-day default explicitly.

### Researched, deliberately not implemented (not statically lintable)

**Compiler-level caching** (`sccache` for Rust/C/C++/CUDA, `ccache` for
C/C++, Gradle's build cache, Maven's local repo). This is a distinct
layer from `PERF001`'s dependency caching — it caches *compiled object
output*, not downloaded packages, and can cut a from-scratch Rust build
from minutes to seconds on a cache hit. In principle detectable
(`cargo build`/`rustc` in `run:` with no `Swatinem/rust-cache` or
`sccache` setup step nearby), but the false-positive risk is real:
distinguishing "this job compiles enough code for a compiler cache to
matter" from "this job runs `cargo check` on three files" needs
information vlotpipe's static YAML view doesn't have. Left as a
candidate for a future, more targeted rule rather than something to ship
speculatively.

**GitHub Actions cache eviction (10 GB default, LRU eviction).**
Real and worth knowing: repos default to a 10 GB total cache size cap
with oldest-first eviction every 24 hours once exceeded, which can cause
"cache thrashing" (a cache written this run gets evicted before the
next run can use it) on repos with many large caches. As of a [November
2025 GitHub change](https://github.blog/changelog/2025-11-20-github-actions-cache-size-can-now-exceed-10-gb-per-repository/),
orgs can now pay to raise the cap and separately configure retention.
**Not lintable**: this is an account/org-level setting, invisible in any
workflow YAML file — vlotpipe has no way to see it from a local scan.
Worth surfacing in a hosted-dashboard context instead (see
`vlotpipe-dashboard/`), where the tool could actually call the cache-usage
API.

**Merge queues** (`merge_group` trigger) reduce redundant CI runs for
repos merging many PRs by batching mergeability checks, and can address
the "stacked PR" case where GitHub docs note [the same workflow can run
many times for one
stack](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/optimizing-ci-for-stacked-pull-requests).
**Not lintable as a blanket rule**: adopting a merge queue is a
repo-workflow choice that depends on PR volume and team size a static
scan can't infer — flagging "no `merge_group` trigger" on every repo
would be noise on the (very common) case of a low-traffic repo that
doesn't need one.

**Monorepo remote build caching** (Turborepo, Nx, Bazel remote cache).
Extends `LEAN003` (path-filter skipping) — these tools go further with
a dependency-graph-aware "only rebuild/retest what actually changed,
using a cache shared across CI runs and even across developers'
machines" model. Detecting *absence* reliably would mean recognizing
`turbo.json`/`nx.json` presence (signals the tool is in use) then
checking for a remote-cache token/config, which starts requiring
multi-file, framework-specific awareness beyond a single workflow file
— a bigger scope decision than fits under `LEAN`, closer to a future
per-ecosystem plugin than a core rule.

### Updated priority for what's left

1. `LEAN003` (monorepo path filters) — still the highest-value unbuilt
   item from round 1, heuristic risk still the blocker.
2. Compiler-cache detection — highest potential *speed* impact of
   anything in round 2, worth a dedicated, narrowly-scoped rule (e.g.
   only fire when a `Cargo.toml`/`CMakeLists.txt` is present at the repo
   root, reducing false-positive risk) rather than the current
   deliberately-deferred broad version.
3. `LEAN005`–`009` — unchanged from round 1, still blocked on a
   Dockerfile parser or explicitly heuristic/low-confidence.
