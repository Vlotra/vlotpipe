# ADR 0002: `rules:` — generic + rule-specific per-code configuration

**Status:** Proposed — not yet implemented. Supersedes an earlier draft
of this same ADR that proposed two separate sections
(`check.cached_runners`, `check.severity`); this revision unifies them
(and the already-shipped `fix:` section — see Migration below) under
one consistent shape instead of growing a new, differently-structured
config section per feature.

## Context

Three real, distinct customization needs came up in quick succession
this session:

1. **`fix.exclude`/`fix.timeout_minutes`** (already shipped this
   session, `internal/config`, `internal/fixer`) — exclude specific
   codes from `--fix`, override the timeout value it inserts.
2. **Runner cache classification** (`PERF001`/`LEAN010` downgrade to
   `info` on an unrecognized `runs-on:` label rather than staying
   silent or firing at full severity — a repo owner who *knows* a
   self-hosted label already has persistent caching, per the real
   `gha-hmak-web` example from `hmak-web2`, should be able to say so
   and get silence, not just a quieter guess).
3. **Severity overrides** (a repo's own risk calculus might disagree
   with a rule's shipped default — treat `SEC001` as `warning` instead
   of `blocker`, or `STRUCT001` as `blocker` instead of `info`).

Building each as its own bespoke config section (`fix:`, then a
proposed `check:` with two unrelated sub-keys) works but doesn't scale:
every future customization need would invent another shape, another
Go struct, another place to look. The actual pattern underneath all
three is the same one: **a property, scoped to one rule code, with a
default the rule already has.** That's worth naming once and reusing.

## Decision

One named section, keyed by rule code:

```yaml
rules:
  SEC001:
    severity: warning        # generic — every rule accepts this
  STRUCT002:
    max_steps: 25            # special — only STRUCT002 knows what this means
  PERF001:
    cached_runners: ["gha-hmak-web", "*"]
  LEAN010:
    cached_runners: ["gha-hmak-web", "*"]
  TIMEOUT001:
    fix: false                # generic — disable *only this code's* autofix
    fix_default: 15            # special — override the value TIMEOUT001's fix inserts
  AZR001:
    fix_default: 15
```

**Generic properties** — meaningful for every rule, same name and
behavior everywhere, so they don't need documenting on each individual
rule page:

- `severity: blocker|warning|info` — replaces the rule's shipped
  `Severity()` for that code. Applied once, early, before the
  `--severity` floor, `select`-based gating, `report.select`-based
  display, and the `report.to` push payload all consume it — downgrading
  `SEC001` to `warning` must also mean it stops failing `check`'s
  default blocker-only gate, not just change color in text output.
- `fix: false` — for a code that's normally in `fixer.FixableCodes`,
  skip the automatic edit entirely; the finding still fires and gets
  reported as usual. (`true`/omitted is the existing default — fix if
  fixable.)

**Special properties** — rule-specific, each one documented on that
rule's own `docs/rules/<CODE>.md` page rather than anywhere central,
since there's no shared meaning across rules to describe once:

- `STRUCT002.max_steps` — the step-count threshold (currently the
  top-level `max_steps_per_job` — see Migration).
- `PERF001.cached_runners` / `LEAN010.cached_runners` — runner labels
  (exact match, or `"*"` wildcard — same convention as `ignore: path:
  "*"`) known to already have persistent caching; suppresses the rule
  entirely for a job on a matching runner, not just a severity
  downgrade, since this is an asserted fact from the repo owner, not a
  heuristic guess. **Deliberately never folds into
  `looksLikeEphemeralRunner`** (the shared classification function
  `SEC010` also uses, for an unrelated attack-surface reason) — a
  runner known to have a cache says nothing about whether it's safe to
  run untrusted fork-PR code on it. Consulted only by `PERF001`/
  `LEAN010`'s own `Check` functions.
- `TIMEOUT001.fix_default` / `AZR001.fix_default` — the value inserted
  by `--fix` (replaces `fix.timeout_minutes` — see Migration).

### Implementation shape

`internal/config.Config` gains `Rules map[string]RuleConfig`, where
`RuleConfig` carries the generic fields (`Severity *string`, `Fix
*bool`) plus a `Raw map[string]any` (or similar) escape hatch for
special properties, decoded via `yaml.Node` rather than a fixed struct
— special properties differ per rule, so a single flat struct with a
field for every rule's every special property doesn't scale the way
the generic ones do. Each rule that wants a special property reads it
out of `cfg.Rules["ITS_OWN_CODE"].Raw` with its own type assertion and
a sensible fallback on absence/wrong type, rather than `config` needing
to know the shape of every rule's special config in advance.

The `Rule` interface (`internal/rules/rules.go`) still only receives
`*model.Pipeline` — unchanged from ADR 0002's original reasoning:
threading config through all ~24 registered rules' signatures for the
handful that need it is too large a blast radius. Rules needing special
properties (`PERF001`, `LEAN010`, `STRUCT002`) get them the same way
`STRUCT002`'s `max_steps_per_job` already works today — called directly
from `cmd/vlotpipe/main.go` as an "ad-hoc scan-time check" outside the
`rules.Run()` registry, or via a package-level setter
(`baseline.SetCachedRunners`) populated once before `rules.Run`,
whichever fits the specific rule's existing call shape. `severity`/`fix`
(the generic properties) apply as a post-processing step over the
already-collected violation list in `run()`, alongside where `select`/
`report.select` filtering already happens — no rule-side change needed
for those two at all.

## Migration

Two already-shipped things move under `rules:` rather than staying
where they are, both while this is still pre-1.0 with no external
users to break:

- **`max_steps_per_job` (top-level)** → `rules.STRUCT002.max_steps`.
- **`fix.exclude`/`fix.timeout_minutes`** (shipped this session) →
  `rules.<CODE>.fix: false` / `rules.<CODE>.fix_default`, one entry per
  code instead of one shared list — `fix.exclude: [TIMEOUT001, AZR001]`
  becomes two entries, `rules.TIMEOUT001.fix: false` and
  `rules.AZR001.fix: false`, which reads more verbosely for the
  "exclude several at once" case but composes correctly with each
  code's own `fix_default`/other special properties living right next
  to it instead of in a separate section.

Whoever implements this should decide whether the old top-level
`max_steps_per_job` and the `fix:` section stay as deprecated aliases
for one release or are a clean break — given no external users exist
yet, a clean break is likely simpler and is the current lean, but
worth a deliberate decision at implementation time rather than
defaulting to either silently.

## Consequences

- `docs/rules/README.md` should document the two generic properties
  once, centrally (`severity`, `fix`), and each rule page with a
  special property documents that property specifically — the same
  split as this ADR's Decision section.
- `docs/SELECTIVE_ENFORCEMENT.md` needs a cross-reference: `select`/
  `report.select` decide *which codes participate*, `rules.<CODE>.severity`
  decides *what severity a participating code counts as* — related but
  distinct, worth being explicit they compose rather than overlap.
- The `.vlotpipe.yml` template (`internal/config`'s `template` const)
  gains a `rules:` example alongside the existing `select`/`report`/
  `fix` ones, once this lands — and the existing `fix:` example in that
  same template needs removing/updating per the Migration section above
  so the template doesn't teach a shape that's about to be superseded.
