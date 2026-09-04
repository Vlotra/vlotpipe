# Vetting run: vitejs/vite

Second real-world vet, deliberately chosen to differ from
`VETTING_RUFF.md` in every way that might exercise different code paths:
JS/TS + pnpm instead of Rust/cargo, heavy `issue`/`pull_request_target`
bot automation instead of a single build pipeline, and — like ruff — a
repo that already runs its own security scanners (`zizmor.yml` and
`semgrep.yml` are both present as workflows), so findings could again be
checked against an adjudicated baseline.

## Method

Same sparse-checkout approach as the ruff run:

```
git clone --depth 1 --filter=blob:none --no-checkout https://github.com/vitejs/vite.git
git sparse-checkout init --cone && git sparse-checkout set .github && git checkout
vlotpipe scan vite/ --format json
```

13 workflow files.

## Result

37 findings, 0 blockers, across `TIMEOUT001` (18), `STRUCT001` (12),
`PERF001` (4), `PERF002` (2), `LEAN002` (1). Notably: **zero `SEC*`
findings** on the first pass. Rather than assume that meant the rules
were silently failing to fire, checked directly against the source
(`grep` for every pattern each `SEC` rule targets) before concluding
anything:

- No `pull_request_target`/`workflow_run` job checks out PR code (2 of
  the 3 files using these triggers explicitly document why, see below).
- No `github.actor ==` checks, no `secrets: inherit`, no
  `toJSON(secrets)`.
- Every `permissions:` block present is `{}` or explicitly scoped; the
  few that aren't are on workflows with no `pull_request` exposure.
- Every `actions/checkout` in a job that doesn't need to push sets
  `persist-credentials: false` explicitly.

vite's maintainers are visibly disciplined here (unsurprising, given they
run zizmor and semgrep themselves) — the zero-`SEC`-findings result held
up as a true negative, not a miss.

## True negative worth calling out: `pull_request_target` used safely

`semantic-pull-request.yml` and `bot.yml` both trigger on
`pull_request_target`, and both carry `# zizmor: ignore[dangerous-triggers]`
comments with an explicit `SAFETY:` justification ("does NOT check out PR
code", "only PR title is read"). `SEC003` correctly stayed silent on
both — it only fires on the checkout-of-untrusted-head *combination*, not
on the trigger alone, matching zizmor's own design rather than
blanket-flagging every `pull_request_target` use. Worth noting because a
naive "flag pull_request_target, period" rule would have produced two
false positives here that a more careful team had already reasoned
through and documented.

## Gap found and fixed: `issue_comment` wasn't in the dangerous-trigger list

`ecosystem-ci-trigger.yml` implements a ChatOps pattern
(`/ecosystem-ci run` as an issue comment triggers a downstream workflow
against the commenter's PR) on the `issue_comment` trigger. This wasn't
in `dangerousTriggers` (`internal/rules/baseline/triggers.go`), which
only listed `pull_request_target` and `workflow_run` — an omission
against `SECURITY_RESEARCH.md`'s own cited source (the OWASP GitHub
Actions Cheat Sheet explicitly calls out `issue_comment` alongside the
other two: any GitHub account can comment on a public issue/PR without
needing repo access, and the resulting `GITHUB_TOKEN`/secrets exposure is
the same).

In this specific workflow the gap was harmless — vite's implementation is
notably careful, manually comparing the PR's head-commit timestamp
against the comment timestamp to reject a TOCTOU attempt, and it never
runs `actions/checkout` on untrusted code at all. But the *rule* had a
real hole: a less careful `issue_comment`-triggered workflow that did
`actions/checkout` with `ref: refs/pull/${{ github.event.issue.number
}}/head` (the standard ChatOps checkout-a-PR-by-number pattern) would
have gone completely undetected.

**Fix**, in `internal/rules/baseline/triggers.go`:
- Added `issue_comment` to `dangerousTriggers` (now covers `SEC003` and
  `SEC012`, both of which gate on `hasDangerousTrigger`).
- Added `event.issue.number` and `refs/pull/` to `refIsUntrustedHead`'s
  match list, since `pull_request.head.sha`-style refs don't exist in an
  `issue_comment` event context — the untrusted-checkout pattern there is
  structurally different (build a `refs/pull/N/head` ref from the issue
  number) and needed its own detection, not just a trigger-list addition.

Re-ran against both vite and the earlier ruff checkout after the fix:
**zero new findings on either** (37 and 93 respectively, unchanged) —
confirms the fix closes a real detection gap without introducing false
positives on two independent real-world codebases. Regression tests
added: "SEC003 fires on issue_comment ChatOps checkout by PR number" and
"SEC003 does not fire on issue_comment with no checkout".

## One confirmed heuristic blind spot (documented, not fixed)

`LEAN002` fired on `prepare-release.yml`'s `fetch-depth: 0`. Unlike the
`LEAN001` false positive found in the ruff run, this one is genuinely
ambiguous rather than wrong: the checkout is immediately followed by
`node scripts/prepare-release.ts`, an external script vlotpipe has no
visibility into. Release-preparation tooling commonly does need full tag
history (to compute the next version, walk changelog-relevant commits,
etc.) — but `LEAN002`'s history-need heuristic only pattern-matches
literal shell text in `run:` (`describe`, `changelog`, `git log`, ...) in
the *same job*, and can't see inside an invoked script. This is an
inherent limit of static YAML analysis, not something worth chasing:
correctly resolving it would require executing or parsing arbitrary
called scripts, which is out of scope for the tool's design. `LEAN002`'s
`info` severity already reflects this — it's a nudge to check, not an
assertion of certainty.

## True positives spot-checked

- **`PERF001`** (4 hits): confirmed each flagged job runs `pnpm install`
  with no caching action and no `cache:`/`package-manager-cache:` input
  set (one, `prepare-release.yml`, explicitly sets
  `package-manager-cache: false`).
- **`TIMEOUT001`** (18), **`PERF002`** (2), **`STRUCT001`** (12):
  consistent with the pattern already established in the ruff run — real
  gaps in timeout/concurrency coverage, and expected per-file noise from
  `STRUCT001` on single-purpose workflows (`publish.yml`, `bot.yml`, ...)
  that legitimately have no test/lint job because `ci.yml` covers that
  separately.

## Takeaway

Two real-world repos in, the pattern holds: false positives cluster
around a handful of *specific, fixable* gaps (managed runner services,
explicit `persist-credentials: true`, and now a missing trigger in the
dangerous-triggers list) rather than being spread evenly across the rule
pack — and each was findable specifically *because* the target repo
already had a mature, disciplined CI setup with its own documented
security trade-offs to compare against. A messier, less-maintained repo
would probably surface more true positives and fewer edge cases; a
cleaner one, like both chosen so far, is better for finding the tool's
own blind spots.
