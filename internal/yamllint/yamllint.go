// Package yamllint implements pipeline-aware YAML style checks — the
// "yamllint for pipelines" half of vlotpipe, distinct from the
// SEC/AZR/PERF/LEAN/STRUCT policy rules in internal/rules/baseline.
// These checks operate on the raw YAML tree directly rather than the
// normalized internal/model, because the concerns here (duplicate keys,
// unquoted ambiguous scalars, raw byte formatting) exist below the
// level the model captures, and apply identically regardless of which
// platform's schema the file happens to use.
//
// Generic yamllint itself has no notion of GitHub Actions or Azure
// Pipelines schema, which cuts both ways: it misses pipeline-specific
// context, and it also produces false noise a pipeline-aware checker
// doesn't have to. The clearest example is yamllint's own "truthy"
// rule, which by default flags GitHub Actions' own required "on:"
// trigger key as an ambiguous boolean-like key — real projects have to
// carry a custom yamllint config just to silence that. YAML005 below
// only ever inspects mapping *values*, never keys, so "on:" is never a
// candidate in the first place; nothing to special-case.
package yamllint

import (
	"bytes"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vlotra/vlotpipe/internal/rules"
)

// truthyWords are YAML 1.1's boolean-like spellings beyond true/false —
// deliberately excluding true/false (and their Title/UPPER variants)
// themselves, since those are the standard, extremely common, correct
// way to write a boolean in CI YAML ("fail-fast: false",
// "continue-on-error: true"); flagging them would bury the real signal
// in constant noise. The go-yaml v3 library this project uses already
// resolves booleans per YAML 1.2 (true/false only) rather than YAML
// 1.1's wider set, so an unquoted "yes"/"on"/"n" etc. parses as a
// literal string *here* — but not every YAML parser in a pipeline's
// life (other tooling, other languages, the platform's own backend)
// necessarily agrees, which is exactly the ambiguity this flags.
var truthyWords = map[string]bool{
	"y": true, "Y": true,
	"n": true, "N": true,
	"yes": true, "Yes": true, "YES": true,
	"no": true, "No": true, "NO": true,
	"on": true, "On": true, "ON": true,
	"off": true, "Off": true, "OFF": true,
}

// Check runs every style check against one file's raw content. path is
// used only to populate Violation.Path.
func Check(path string, raw []byte) []rules.Violation {
	var out []rules.Violation
	out = append(out, checkTrailingWhitespace(path, raw)...)
	out = append(out, checkFinalNewline(path, raw)...)

	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		// A genuine syntax error is reported separately (YAML001, in
		// cmd/vlotpipe) by the caller, which already has this error
		// from its own parse attempt — nothing more to add here.
		return out
	}
	if len(root.Content) != 0 {
		out = append(out, walk(path, root.Content[0])...)
	}
	return out
}

// walk recurses through every node, checking each mapping node's own
// key/value pairs for duplicate keys and truthy-looking values, then
// descending into every child (mapping values, sequence items) so
// nested blocks — a job's "with:", a step's "env:", Azure's
// "variables:" — get the same treatment as the document root.
func walk(path string, n *yaml.Node) []rules.Violation {
	if n == nil {
		return nil
	}
	var out []rules.Violation
	if n.Kind == yaml.MappingNode {
		seen := map[string]*yaml.Node{}
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i], n.Content[i+1]

			if prev, ok := seen[key.Value]; ok {
				out = append(out, rules.Violation{
					Code:     "YAML002",
					Rule:     "duplicate-key",
					Severity: rules.SeverityWarning,
					Message: fmt.Sprintf(
						"duplicate key %q (first seen at line %d); the earlier value is silently overwritten, not merged",
						key.Value, prev.Line,
					),
					Path: path,
					Line: key.Line,
					Col:  key.Column,
				})
			} else {
				seen[key.Value] = key
			}

			// Style == 0 means "plain scalar", i.e. genuinely unquoted
			// — a value already written as 'yes' or "on" was a
			// deliberate string and isn't ambiguous to begin with.
			if val.Kind == yaml.ScalarNode && val.Style == 0 && truthyWords[val.Value] {
				out = append(out, rules.Violation{
					Code:     "YAML005",
					Rule:     "truthy-value",
					Severity: rules.SeverityInfo,
					Message: fmt.Sprintf(
						"%q is an unquoted YAML boolean-like value; not every YAML parser resolves it the same way — quote it (%q) if a literal string was intended, or use true/false if a boolean was intended",
						val.Value, val.Value,
					),
					Path: path,
					Line: val.Line,
					Col:  val.Column,
				})
			}
		}
	}
	for _, c := range n.Content {
		out = append(out, walk(path, c)...)
	}
	return out
}

func checkTrailingWhitespace(path string, raw []byte) []rules.Violation {
	var out []rules.Violation
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == line {
			continue
		}
		out = append(out, rules.Violation{
			Code:     "YAML003",
			Rule:     "trailing-whitespace",
			Severity: rules.SeverityInfo,
			Message:  "trailing whitespace",
			Path:     path,
			Line:     i + 1,
			Col:      len(trimmed) + 1,
		})
	}
	return out
}

func checkFinalNewline(path string, raw []byte) []rules.Violation {
	if len(raw) == 0 {
		return nil
	}
	if !bytes.HasSuffix(raw, []byte("\n")) {
		return []rules.Violation{{
			Code:     "YAML004",
			Rule:     "final-newline",
			Severity: rules.SeverityInfo,
			Message:  "file does not end with a newline",
			Path:     path,
			Line:     bytes.Count(raw, []byte("\n")) + 1,
			Col:      1,
		}}
	}
	if bytes.HasSuffix(raw, []byte("\n\n")) {
		return []rules.Violation{{
			Code:     "YAML004",
			Rule:     "final-newline",
			Severity: rules.SeverityInfo,
			Message:  "extra blank line(s) at end of file",
			Path:     path,
			Line:     bytes.Count(raw, []byte("\n")),
			Col:      1,
		}}
	}
	return nil
}
