# Selective enforcement: `select` and `report.select`

`vlotpipe check`'s exit code is, by default, all-or-nothing: any
blocker-severity finding, from any rule, fails the build. That's fine
for a repo adopting vlotpipe from a clean slate. It's a real barrier for
an existing repo with debt — turning `check` into a required status
check on day one would fail every open PR at once, which is usually
enough to get the whole idea reverted before it gets a fair trial.

`select` and `report.select` in `.vlotpipe.yml` exist to make the
rollout gradual instead: gate the build on a small, currently-clean set
of rules today, while still seeing every finding so the team knows what
to widen the gate to next.

## The two knobs, and why they don't affect each other

```yaml
# Which rule codes/prefixes can fail "check". Omitted = every rule can
# gate (today's default behavior).
select:
  - SEC
  - AZR002

# A separate, named section — like ruff's own separate "lint" and
# "format" config — with its own independent select. Omitted = every
# rule is shown (today's default behavior). Narrowing the gate above
# does NOT narrow this; that's the whole point.
report:
  select:
    - SEC
    - AZR002
    - TIMEOUT001
```

Both use the same matching rule: an entry is either an exact code
(`SEC001` matches only that code) or a category prefix (`SEC` matches
every `SEC*` code) — the same convention as ruff's own `--select`.

`select` and `report.select` are independent on purpose. If narrowing
`select` also narrowed the report, vlotpipe would only ever be able to
show you what it's already gating on — a strict subset of reality,
useless for planning what to gate on *next*. Keeping them separate is
what makes this workflow possible:

1. **Start**: nothing configured. `check` gates on every rule, `scan`
   shows every finding — today's behavior, unchanged.
2. **Adopt**: pick the small set of rules the repo is already clean on
   (or close to), put it in `select`. `check` now only fails on those —
   safe to turn into a required status check immediately, even with a
   real backlog elsewhere.
3. **Plan**: leave `report.select` unset (or set it broader than
   `select`). Every scan still shows the *entire* backlog — every rule,
   every code — so the team can see exactly what's left and decide what
   to widen `select` to next, without that backlog ever blocking a
   merge in the meantime.
4. **Widen**: as codes get paid down, add them to `select`. Repeat.

## Interaction with `ignore:`

`ignore:` (suppress a specific code on a specific path, with a reason)
still applies first, before either `select` filters anything. A
suppressed finding means "this isn't a real problem" — that should hold
everywhere, including in a broader `report.select` view and in a
[`report.to`](INTEGRATIONS.md#pushing-to-a-dashboard) push. `select` and
`report.select` narrow *which remaining, real findings* gate the build
or get shown; they don't reach back and un-suppress anything.

## CLI overrides

`--select` and `--report-select` (comma-separated) override
`.vlotpipe.yml`'s `select:`/`report.select:` for a single invocation —
useful for a one-off local check without editing the committed config:

```
vlotpipe check . --select SEC001,AZR002
vlotpipe scan . --report-select SEC
```

## What this isn't

Not a replacement for `--severity`. `--severity` is a floor by
*severity tier* (blocker/warning/info) — orthogonal to `select`, which
is an allow-list by *rule code*. They compose: `--select SEC001
--severity warning` gates only on `SEC001`, but only if it's at least
warning severity (which, being a blocker-severity rule, it always is —
a more realistic example is combining a broad `select` with a raised
`--severity` floor to only report the more serious subset of a wide
selection).
