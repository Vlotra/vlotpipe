# ADR 0001: Selective enforcement (`select`/`report.select`) and dashboard push (`report.to`)

**Status:** Accepted, implemented (`internal/config`, `cmd/vlotpipe/main.go`, `internal/pushreport`).

## Context

`vlotpipe check`'s exit code was, until this decision, all-or-nothing:
any blocker-severity finding, from any rule, failed the build. That's
fine for a repo adopting vlotpipe from a clean slate. It's a real
barrier for an existing repo with debt — turning `check` into a
required status check would fail every open PR on day one, which is
usually enough to get the whole idea reverted before it gets a fair
trial. ESLint's per-rule error/warn split and ruff's own `--select`
solve this the same way: let a team narrow which rules actually gate,
independent of what's still visible.

Separately, there's a standing product direction — the sibling
`vlotpipe-dashboard` prototype — for a hosted layer aggregating findings
across a team's repos and branches. That prototype only ever *pulls*
data (`collect` runs `vlotpipe scan` + the GitHub API locally and emits
one JSON file); its own README names "no push/webhook ingestion" as a
known gap. Closing that gap needed a client-side contract to build
against.

## Decision

Three independent, optional `.vlotpipe.yml` settings:

- **`select` (top-level list)** — which rule codes/prefixes can fail
  `check`. Empty/omitted = every rule can gate (unchanged default
  behavior). Matching is prefix-based (`"SEC001"` matches only that
  code, `"SEC"` matches every `SEC*` code) — ruff's own `--select`
  convention, reused rather than inventing a separate "category"
  concept.
- **`report.select` (named section)** — which rule codes/prefixes appear
  in `scan`/`check`'s own output. **Deliberately does not fall back to
  `select`** — the two are parallel, independently-defaulting sections
  (matching ruff's own separate `lint`/`format` config), not one
  inheriting from the other. This is what makes "gate on a small set,
  see the full backlog" possible: narrowing the gate must never silently
  narrow what's visible.
- **`report.to` (same named section)** — a URL to POST the *complete,
  unfiltered* finding set to after a scan, never narrowed by either
  `select` field above. Resolved flag → `VLOTPIPE_REPORT_TO` env var →
  config, first non-empty wins. The **auth token has no config-file
  field at all** — `VLOTPIPE_REPORT_TOKEN` (env var) only, since
  `.vlotpipe.yml` is a committed file and a hardcoded token there is
  exactly the `SEC002` pattern this tool itself flags in pipeline YAML.

Implementation shape: `cmd/vlotpipe/main.go`'s `run()` collects every
finding once (`everything`, filtered only by `ignore:`/inline
suppression — those mean "not a real problem," which should hold
everywhere), then derives three independent views from it: `all`
(display: severity floor + `report.select`), `blockers` (gate: blocker
severity + `select`), and the push payload (`everything`, always
complete). Push failures are logged as a warning and never change
`check`'s exit code or fail the scan — an optional, separately-operated
dashboard being down must never break the actual lint gate.

## Consequences

- A `.vlotpipe.yml` with neither `select` nor `report` set is unchanged
  from pre-ADR behavior — this was verified, not assumed (full existing
  test suite green before any new test was added).
- `internal/pushreport` defines the client-side payload contract
  (`Payload{Repo, Branch, Commit, ScannedAt, FilesScanned, Violations}`,
  JSON, optional bearer auth) with no real server to talk to yet. See
  `vlotpipe-dashboard`'s own ADR 0001, which records the corresponding
  server-side commitment.
- `select`/`report.select` are a new, third suppression-adjacent
  mechanism alongside `ignore:` (per-code/path suppression with a
  reason) and inline `# vlotpipe: ignore` comments. They compose:
  `ignore:` always applies first, inside each rule check, before either
  `select` filters the remainder.
