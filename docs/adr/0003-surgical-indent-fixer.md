# ADR 0003: Surgical indent-only fixing, decoupled from key reordering

**Status:** Accepted, implemented (`internal/formatter.FormatIndentOnly`,
`cmd/vlotpipe/main.go`'s `runFormat`).

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
every run. **Decided at implementation time: `vlotpipe format
--reorder-keys`** — the conservative behavior (indent-only) is the
bare `format` default, so the flag names the more disruptive path being
opted *into*, rather than naming the safe path as if it needed an
explanation. Reordering inherently means moving lines, so it will
always produce a real diff for whatever it touches — the point isn't to
make that diff smaller, it's to make it something the caller explicitly
asked for rather than a side effect of fixing indentation.

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

- `docs/GETTING_STARTED.md`'s "Auto-fixing and formatting" section,
  `README.md`, and `docs/INTEGRATIONS.md` no longer warn that `format`'s
  first run "will produce a large diff" — that warning now describes
  `--reorder-keys` specifically, not the default. `action.yml` turned
  out not to reference `format`'s disruptiveness at all (checked at
  implementation time, this ADR's own prediction was wrong on that
  point) — nothing to change there.
- `.pre-commit-hooks.yaml`'s `vlotpipe-format` hook description was
  updated to describe the surgical default and point at `--reorder-keys`
  for the bigger-diff alternative.
- Verified with a real fixture (not the full 39-file/4-repo sample —
  that would need those external repos checked out again, which wasn't
  practical from this codebase alone): a single wrongly-indented
  `branches:` line under an otherwise well-formatted GitHub Actions
  workflow produces exactly a 1-line diff under the new default, versus
  the same file's blank lines vanishing and two keys reordering under
  the old full re-encode (still available via `--reorder-keys`).
  `internal/formatter/formatter_test.go`'s
  `TestFormatIndentOnlyTouchesOnlyTheWrongBlock` covers the same claim
  as an automated test: a sibling job that's already canonical survives
  byte for byte while only the inconsistently-indented job's lines move.
- **Decided at implementation time: `--check` stays file-level**, same
  as `Format`'s existing `--check` — no line-level diff preview yet.
  The ADR's own Consequences left this open rather than mandating it;
  scoping a diff-style preview (unified-diff output? a `--verbose`
  flag?) is deferred to a future change if it turns out to matter in
  practice, rather than guessed at here.
