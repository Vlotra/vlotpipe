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
- **Full local detail, not a count-only teaser** (revised after the
  first version of this ADR shipped a bare cluster count and got
  challenged on it, correctly). `scan`/`check` print every cluster
  found, with each member's exact `path:line` and job name — the same
  precision every other finding in this tool gets. Withholding *which*
  jobs matched, when the computation is 100% local and already done for
  free, doesn't protect anything paid; it just makes the free tier worse
  than every other linter output in this codebase for no reason. The
  actual paid boundary was always aggregation *across many repos*, not
  secrecy about a single scan's own output — no local scan of one repo
  can tell you "this job also appears in 20 other repos across your
  org," regardless of how much detail it prints about itself. Still text
  format only (JSON output unchanged) — that boundary stands, since it's
  about not growing the machine-readable contract prematurely, not about
  hiding data.
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

## One known imprecision (tried a fix, reverted it, documenting instead)

Vetting against a real multi-language monorepo (PHP/Yii2 backend +
Angular frontend, 8 own workflow files) surfaced a shape neither the
synthetic tests nor the source write-ups anticipated: two jobs running
**completely different tools** (two different security scanners, one
Nuclei-based, one ZAP-based) clustered as "duplicate jobs." On
inspection, they aren't duplicated — they share one copy-pasted,
multi-word `run:` guard clause ("skip this job if `$TARGET_URL` is
unset") wrapped around otherwise-unrelated automation. The guard
clause's word count let it outvote the jobs' differing `uses:` actions
in `simhash`'s per-token weighting.

The obvious-looking fix — give each *step* equal total weight
regardless of its word count, so a verbose `run:` script can't out-vote
a concise `uses:` step — was implemented, tested (a regression test
reproducing this exact shape passed), and then re-vetted against the
same real repo before being kept. That re-vet is what caught the
problem: on the same corpus, cluster count went from 2 to 4, and one of
the new clusters chained together four jobs with genuinely different
purposes (a static-analysis job, a dependency-audit job, and two real
test jobs) — all mutually >90% similar under equal-step-weighting,
confirmed by checking pairwise similarity directly rather than trusting
the cluster count alone. The common thread: PHP CI jobs in this repo
share a `checkout` + `setup-php` + `composer install` prefix, which is
routine environment setup, not duplication — but at 4-6 steps per job,
that prefix is 50-75% of a job's steps, and equal-per-step weighting
let routine setup dominate just as badly as verbose `run:` text did
before, in the opposite direction.

**Reverted to the original word-count-weighted scheme** — on this same
real corpus it produced fewer false groupings (1 questionable pair, not
a 4-job false clique) — and left the Nuclei/ZAP pair as a known,
undecided imprecision, the same call the ruff vetting run made for
`LEAN001` (see `VETTING_RUFF.md`) rather than shipping a fix that
regressed on the exact data it was tested against. Two follow-ups this
points at, neither a quick patch:

- **IDF-style reweighting** (downweight tokens common across the
  corpus, upweight rare ones) sounds like the standard fix, but doesn't
  actually resolve this case: the shared guard clause is rare within a
  *single scan's* small corpus (by construction — it's shared between
  exactly the two jobs in question), so IDF would weight it *up*, not
  down. It also cuts against this ADR's own portability requirement —
  `Chunk.Signature` needs to mean the same thing across separate
  `vlotpipe` invocations for the dashboard to compare fingerprints
  cross-repo (see `Chunk`'s doc comment), and per-scan corpus-relative
  weighting breaks that outright. If IDF is ever worth doing, it's a
  dashboard-side computation over the full cross-repo corpus, not a
  CLI-side one — another item for the already-deferred list above.
- **Step-level granularity** (already deferred, see above) is the
  actually-correct fix: it would report "these two jobs share one
  duplicated step-window" rather than forcing a binary "these two whole
  jobs are/aren't duplicates" verdict that neither weighting scheme can
  give an honest answer to.

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
- **DUP001, added after real usage:** printing cluster detail (above)
  immediately surfaced the need to suppress a specific finding — the
  documented Nuclei/ZAP imprecision, in practice. A duplicate cluster
  isn't a `rules.Violation` (it can span two files; a `Violation`'s one
  `Path` can't represent that), so it needed its own suppression path
  rather than reusing the violation pipeline outright. Landed as: a
  `fingerprint.Code = "DUP001"` constant; inline `# vlotpipe:
  ignore[DUP001]` on a job's key line, checked inside `BuildChunks` via
  `model.Pipeline.IsSuppressed` (zero new parser code — it already
  parses this comment for every rule); and `.vlotpipe.yml`'s path-based
  `ignore:`, applied by the caller in `main.go` rather than inside
  `fingerprint` itself, preserving the "no config dependency" property
  above. See `docs/rules/DUP001.md`.
