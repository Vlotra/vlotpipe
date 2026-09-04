# Custom rules

vlotpipe's 24 baseline rules are compiled into the binary — there's no
plugin system, no scripting runtime. For most policy, that's fine: the
baseline pack covers the well-established, broadly-applicable cases. But
every org eventually has a policy that's specific to *them*, and
shouldn't need a fork and a rebuild to express. `custom_rules:` in
`.vlotpipe.yml` is that escape hatch.

## The motivating example

`TIMEOUT001` checks one thing: is `timeout-minutes` set at all. It has
no opinion on *what* it's set to. Someone can technically satisfy it
while missing the point entirely:

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 360 # satisfies TIMEOUT001; is not actually a bound
```

360 is GitHub's own default cap — setting it explicitly to that value is
functionally identical to not setting it at all. No baseline rule can
catch this without becoming opinionated about a specific number, which
is exactly the kind of judgment call that should live in a company's own
config, not in vlotpipe's source:

```yaml
# .vlotpipe.yml
custom_rules:
  - code: ORG001
    severity: warning
    scope: job
    field: timeout-minutes
    equals: "360"
    message: "timeout-minutes is set to GitHub's own default; pick a real bound"
```

## Shape of a rule

```yaml
custom_rules:
  - code: <your code>       # required — any string; ORG001-style is a reasonable convention
    severity: <severity>    # required — blocker, warning, or info
    message: <message>      # required — shown in the report
    scope: job | step       # optional, defaults to "job"
    field: <field name>     # required — see the field list below
    equals: <value>         # optional
    not_equals: <value>     # optional
    matches: <regex>        # optional
    exists: true | false    # optional
```

At least one of `equals`, `not_equals`, `matches`, `exists` is required.
Specifying more than one combines them with AND — `exists: true` plus
`equals: "x"` requires the field to be both present and equal to `"x"`.

A bad config (unknown field, invalid severity, missing matcher, a regex
that doesn't compile) fails the scan immediately with a message naming
the offending rule's `code` — not a silent no-op.

## Supported fields

Deliberately a small, explicit, curated list rather than every Go struct
field reflected automatically — this is the contract config authors
write against, so it should stay stable and legible.

**`scope: job`** (the default):

| Field | Matches against |
| --- | --- |
| `timeout-minutes` | the job's `timeout-minutes` value, as a string; absent (not "exists") if unset |
| `runs-on` | the job's `runs-on` value |
| `name` | the job's `name:` (or its ID, if `name:` isn't set) |
| `id` | the job's YAML key under `jobs:` |
| `if` | the job's `if:` condition |
| `permissions-set` | `"true"`/`"false"` — whether the job declares its own `permissions:` block |

**`scope: step`**:

| Field | Matches against |
| --- | --- |
| `uses` | the step's `uses:` value |
| `run` | the step's `run:` script |
| `name` | the step's `name:` |
| `if` | the step's `if:` condition |

## More examples

Requiring every third-party action to come from an internally-vetted
mirror namespace:

```yaml
custom_rules:
  - code: ORG002
    severity: blocker
    scope: step
    field: uses
    matches: '^(actions/|internal-org/)'
    message: "third-party actions must be actions/* or go through the internal-org/* mirror"
```

Note `matches` fires when the field *contains* a match (`regexp.MatchString`
semantics), not on a full match — anchor with `^`/`$` if that's not what
you want. There's no `not_matches`: Go's `regexp` package (RE2, the same
engine Go itself uses) doesn't support negative lookahead, so "flag
anything that *doesn't* match this pattern" can't be written as one
`matches` regex. `not_equals` covers the exact-string case; a
does-this-follow-a-convention check across arbitrary patterns is outside
what a single field comparison can express — see "What this doesn't
replace" below.

Flagging any self-hosted-labeled runner outside a known-safe set, using
`not_equals` for the one-exact-value case:

```yaml
custom_rules:
  - code: ORG004
    severity: warning
    field: runs-on
    matches: '^self-hosted'
    not_equals: "self-hosted-approved-pool"
    message: "self-hosted runner label isn't the approved pool"
```

## What this doesn't replace

This is deliberately field-comparison only — no boolean combinators
across multiple fields, no cross-job logic, no access to the full
untrusted-context detection `SEC004`/`SEC012` use internally. A policy
that needs "if trigger is X *and* step Y does Z *and* permissions don't
include W" is baseline-rule territory, not `custom_rules:` territory —
open an issue or contribute a Go rule (see `internal/rules/baseline/`
for the pattern every existing rule follows) rather than trying to force
it through field matchers.

## Suppressing a custom rule's own finding

Works exactly like any baseline rule — `.vlotpipe.yml`'s `ignore:` list
or an inline `# vlotpipe: ignore[ORG001]` comment, since custom rule
violations flow through the same severity floor, ignore list, and
inline-suppression logic as everything else.
