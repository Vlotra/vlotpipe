# ADR 0003: Surgical indent-only fixing, decoupled from key reordering

**Status:** Proposed — not yet implemented.

## Context

Real-world testing (`vlotpipe format` against `hmak-web2`, then against
a 39-file/4-repo sample of already-vetted real projects — ruff, vite,
gin-vue-blog, aitos) surfaced the actual pain point, and it isn't file
size. It's diff size relative to what actually changed:

- On `hmak-web2`, `format` produced a 417-deletion/242-insertion diff
  across 8 files for what was, semantically, "make the indentation
  consistent." Every line moved, including ones that were already fine.
- Net byte change across the 39-file sample was −0.32% — consistently
  small and in the expected direction, so the *size* complaint doesn't
  hold up under a bigger sample. The *diff churn* complaint does: a
  full parse-and-re-encode touches every line unconditionally, whether
  or not that line needed to change.

Two root causes, independent of each other:

1. **Blank lines are lost, universally.** `yaml.v3`'s node model
   doesn't track them at all — any re-encode strips every blank line,
   regardless of whether anything else in the file needed fixing. This
   alone accounts for a large fraction of the diff noise on a
   well-formatted file that just has one or two indentation slips.
2. **Key reordering and indentation are bundled into one operation.**
   A file with already-correct key order but slightly-off indentation
   (or vice versa) gets the *entire* file re-encoded to fix either,
   since `formatter.Format` doesn't distinguish the two concerns.

## Decision

Split `format`'s current behavior into two independently-invokable
operations, and make the gentler one the default:

**Indentation normalization (new default)** — a surgical text patch,
not a re-encode, using the same pattern already proven in
`internal/fixer`: parse the file once (`yaml.v3`, as today, purely to
get each node's `Line`/`Column`), compute the *canonical* indent for
each line from its structural nesting depth, and rewrite **only the
leading whitespace of lines whose current indent disagrees** with that.
Blank lines, comments, quote style, key order, and every line that's
already correctly indented are left exactly as they were — byte for
byte. A file with one inconsistently-indented block produces a
one-block diff, not a whole-file rewrite.

**Key reordering (opt-in)** — the canonical-key-order behavior
`format` has today, kept available but no longer bundled silently into
every run. Proposed as `vlotpipe format --reorder-keys` (name TBD at
implementation time — could also be the inverse, e.g.
`--indent-only` as the conservative flag and full reorder as the
default, worth deciding deliberately rather than defaulting either way
without discussion). Reordering inherently means moving lines, so it
will always produce a real diff for whatever it touches — the point
isn't to make that diff smaller, it's to make it something the caller
explicitly asked for rather than a side effect of fixing indentation.

### Implementation shape

Lives in `internal/formatter` alongside the existing `Format` function,
or as a new sibling function (`FormatIndentOnly` or similar) — the two
share the same parse step but diverge after that. The line-by-line
"what does this line's indent need to be" computation reuses the same
kind of column-from-parent-node arithmetic `internal/fixer` already
does for insertion indentation (`jobNode.Content[0].Column`, etc.), just
applied to *every* line in the file rather than one insertion point.
Multi-line block scalars (`run: |`, `script: |`) need explicit
handling: their content's indentation is semantically part of the
string, not a structural YAML indent level, and must never be
"corrected" the way a mapping/sequence line would be.

`applyInsertions`-style line-splicing (already in `internal/fixer`)
doesn't directly apply here — this isn't inserting new lines, it's
replacing the leading-whitespace prefix of existing ones — but the
"never touch anything the analysis doesn't explicitly flag" discipline
carries over directly.

## Consequences

- `docs/GETTING_STARTED.md`'s "Auto-fixing and formatting" section and
  `docs/INTEGRATIONS.md` both currently warn that `format`'s first run
  against a hand-formatted file "will produce a large diff." That
  warning becomes wrong for the new default and needs rewriting once
  this ships — the whole point is that it stops being true.
- `.pre-commit-hooks.yaml`'s `vlotpipe-format` hook description
  ("bigger diffs, see below") and `action.yml` docs referencing
  `format`'s disruptiveness both need the same update.
- Verification, once built: re-run the exact same 39-file/4-repo sample
  this ADR's Context section used, and confirm the diff line count
  drops sharply (not just the byte delta, which was never the real
  problem) while `git diff --stat` on a file with a genuinely
  inconsistent block still shows a real, correctly-scoped change for
  that block specifically.
- Worth deciding at implementation time whether `--check` (already
  supported) needs to report *which lines* would change for the
  indent-only mode, not just which files — since the whole feature is
  about making the change surgical, the preview should probably be
  equally surgical (a diff-style preview) rather than just a file list.
