# ADR 0004: Duplicate job fingerprinting (`internal/fingerprint`) as the first Insights slice

**Status:** Accepted, implemented (`internal/fingerprint`, `internal/pushreport`, `cmd/vlotpipe/main.go`).

## Context

vlotpipe's `check`/`scan` answer "is this one file compliant?" per file.
There's a separate, recurring pattern the baseline rule pack can't see at
all: the same job — a "test" job, a "deploy" job — copy-pasted across
several workflow files (or several repos) with only cosmetic drift: a
bumped version pin, a different secret reference, a tweaked timeout. That
drift is exactly the interesting signal (it means the copies have started
rotting independently), but no per-file rule can catch it, since catching
it requires comparing files against *each other*.

This maps onto the standing "sell a hosted dashboard" direction already on
record in ADR 0001: the open-source CLI computes something locally, an
opt-in `report.to` push carries it to a hosted layer, and cross-repo
aggregation — the part that's actually hard to replicate and actually
worth paying for — lives server-side in the sibling `vlotpipe-dashboard`
project. Two independent write-ups arrived at the same shape (MinHash/
SimHash-style fingerprinting of pipeline chunks, clustered centrally,
surfaced as a "boilerplate ratio" + golden-template suggestion), which is
the confirmation this was worth building rather than two people guessing
the same wrong answer.

## Decision

Ship the free-tier, local-only slice now; defer clustering/drift-diff/
template-generation to the dashboard, matching how ADR 0001 shipped
`pushreport`'s client contract well before a server existed to receive it.

**Scope of this slice:**

- **Job-level chunking only.** Both source write-ups flagged granularity
  (whole-file vs. per-job vs. per-step sliding window) as unresolved.
  Job-level is the sweet spot named in the more detailed of the two specs
  — a single step (`actions/checkout`) is too fine to be interesting
  alone; a whole file conflates unrelated jobs. Step-level sliding-window
  detection (catching a partial "checkout + setup-node + cache" reused
  inside otherwise-different jobs) is real value left on the table, but
  needs the threshold-calibration work below first — adding a second
  granularity before the first one is validated against real data would
  double the surface to recalibrate blind.
- **simhash over MinHash.** Both write-ups reference MinHash/LSH banding,
  which is the right tool once you're comparing across thousands of repos
  without O(n²) cost — the dashboard's problem. Locally, one repo's job
  count is small enough that O(n²) pairwise comparison is fine (see
  `fingerprint.Cluster`), and a single 64-bit simhash per job is simpler
  to get right than a multi-hash MinHash signature + banding scheme for a
  problem that doesn't have the scale to need it yet.
- **Normalization is deliberately narrow**, not full AST-aware
  parameterization: a `uses:` step contributes its action name with the
  version pin stripped; a `run:` step contributes whitespace-collapsed
  text with `${{ ... }}` expression blocks replaced by a placeholder. This
  catches the two drift patterns named in both write-ups (stale version
  pins, differing secret/ref names) without trying to guess which bare
  literals are "really" variables (a repo name typed into a `run:` script
  vs. a coincidentally repo-name-shaped string) — that's the "language-
  awareness" open question neither write-up resolved, left alone rather
  than guessed at.
- **CLI teaser, not a report.** `scan`/`check` print one line — a cluster
  count, nothing else — only for the default text format and only when
  clusters exist (a "0 found" line is noise). No file/job detail, no
  JSON field, no `--format json` output changed. This is deliberately
  the minimum that proves the mechanism works and creates upsell pull,
  short of doing the dashboard's job for it.
- **Fingerprints ride the existing push contract.** `pushreport.Payload`
  gained a `Fingerprints []fingerprint.Chunk` field alongside
  `Violations` — same "always the complete, unfiltered set" rule ADR 0001
  established, since narrowing this defeats the point of centralizing it.
  A `Chunk` is a signature plus a file/job pointer, never step content:
  the privacy property both write-ups called out (raw YAML — deploy
  targets, secret names — never leaves the scanning machine) falls out of
  the type itself, not a policy someone has to remember to apply.

**Deliberately not built here** (dashboard-side, later):

- Cross-repo/cross-scan clustering (this ADR's `Cluster` only ever sees
  one `vlotpipe` invocation's jobs)
- Drift diffing within a cluster (which variant is "the secure one")
- Golden-template synthesis and the "N files → this template" migration
  preview
- The "boilerplate ratio" report-card metric
- Threshold tuning against a real corpus — `DefaultThreshold = 0.90` is a
  reasoned starting point (documented inline in `fingerprint.go`), not a
  calibrated one

## Consequences

- `internal/fingerprint` depends only on `internal/model` — no rule
  engine, no config, no I/O — so it's trivially reusable from the
  dashboard side later without dragging in CLI-only concerns.
- The teaser is silent by default (nothing prints when nothing clusters),
  so this ships with zero behavior change for any repo whose jobs are
  actually distinct — verified via `TestRunOmitsDuplicateTeaserWhenNoClustersFound`.
- Self-hosting/on-prem for compliance-sensitive customers (a real open
  question from the source write-ups — e.g. banking) is unaffected either
  way by this ADR: nothing here requires the hosted dashboard to exist,
  since `Chunk` fingerprints are computed and usable (for the local
  teaser) with `report.to` never configured at all.
