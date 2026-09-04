// Package fixer implements "vlotpipe scan/check --fix": mechanical,
// unambiguous edits for a small, deliberately curated subset of rules.
//
// Not every rule gets an autofix. A fix belongs here only if it needs no
// judgment call — inserting a brand-new key whose value is a reasonable,
// clearly-labeled default, never guessing at business logic (what a
// timeout *should* be, what a secret's real name is) and never touching
// existing content. Rules like SEC001 (needs a network call to resolve
// a tag to a SHA) or SEC002 (there's no way to know where the real
// secret should live) are permanently out of scope for this reason, not
// because they were skipped for time.
//
// This package re-parses the raw file into a yaml.Node tree of its own —
// separate from the normalized internal/model — because a fix is a
// precise textual edit that must preserve every byte of the file it
// isn't touching (comments, quote style, blank lines). The normalized
// model exists so rules can reason about a pipeline without caring which
// platform it came from; the raw tree exists here because a fix cares
// about nothing but the platform's own exact YAML shape.
package fixer

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vlotra/vlotpipe/internal/model"
	azureparser "github.com/vlotra/vlotpipe/internal/parser/azure"
	"github.com/vlotra/vlotpipe/internal/rules"
)

// FixableCodes lists every rule code this package can fix, for reporting
// purposes (e.g. "vlotpipe --fix" help text, --fix-dry-run summaries).
var FixableCodes = map[string]bool{
	"TIMEOUT001": true,
	"AZR001":     true,
	"SEC006":     true,
	"LEAN011":    true,
	"PERF002":    true,
}

// defaultTimeoutMinutes is what TIMEOUT001's and AZR001's fixes insert
// when Options.TimeoutMinutes is left at its zero value.
const defaultTimeoutMinutes = 30

// Options configures how Fix applies its edits. The zero value
// (Options{}) reproduces the original fixed behavior — 30-minute
// timeouts — so existing callers don't need to change to keep working.
type Options struct {
	// TimeoutMinutesByCode overrides the value inserted for TIMEOUT001
	// and/or AZR001, keyed by code (.vlotpipe.yml's
	// rules.TIMEOUT001.fix_default / rules.AZR001.fix_default — the two
	// can differ from each other). A missing or <= 0 entry for a code
	// falls back to defaultTimeoutMinutes.
	TimeoutMinutesByCode map[string]int
}

// timeoutFor resolves the timeout value to insert for code.
func (o Options) timeoutFor(code string) int {
	if v, ok := o.TimeoutMinutesByCode[code]; ok && v > 0 {
		return v
	}
	return defaultTimeoutMinutes
}

// insertion is "add this text as new whole line(s), immediately after
// physical line `after`" (1-indexed, in terms of the original file's own
// line numbers — every insertion's `after` is computed against the same
// unmodified line numbering before any of them are applied).
type insertion struct {
	after int
	text  string // one or more lines, joined by "\n"
}

// Fix applies every fixable violation in violations to raw and returns
// the edited file, plus how many violations were actually fixed (a
// violation whose shape doesn't match what the fixer expects — e.g. an
// existing "with:" block written in flow style — is silently left
// unfixed rather than risk producing invalid YAML). Violations the
// caller doesn't want auto-fixed (fix.exclude in .vlotpipe.yml) should
// already be filtered out of violations before calling Fix — this
// package only knows about the mechanics of a fix, not repo policy
// about which ones to skip.
func Fix(platform model.Platform, raw []byte, violations []rules.Violation, opts Options) ([]byte, int, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return raw, 0, err
	}
	if len(root.Content) == 0 || root.Content[0].Kind != yaml.MappingNode {
		return raw, 0, nil
	}
	doc := root.Content[0]

	var insertions []insertion
	for _, v := range violations {
		var ins *insertion
		switch v.Code {
		case "TIMEOUT001":
			ins = fixGHJobKey(doc, v, fmt.Sprintf("timeout-minutes: %d", opts.timeoutFor("TIMEOUT001")))
		case "AZR001":
			ins = fixAzureJobKey(doc, v, fmt.Sprintf("timeoutInMinutes: %d", opts.timeoutFor("AZR001")))
		case "SEC006":
			ins = fixGHStepWithKey(doc, v, "persist-credentials", "false")
		case "LEAN011":
			// Not 90 — that's the existing default this rule exists to
			// question. Inserting it back would silence the finding
			// without changing any actual behavior. 7 days is a
			// deliberately shorter, still-reviewable placeholder.
			ins = fixGHStepWithKey(doc, v, "retention-days", "7")
		case "PERF002":
			ins = fixGHConcurrency(doc)
		}
		if ins != nil {
			insertions = append(insertions, *ins)
		}
	}
	if len(insertions) == 0 {
		return raw, 0, nil
	}
	return applyInsertions(raw, insertions), len(insertions), nil
}

func fixGHJobKey(doc *yaml.Node, v rules.Violation, line string) *insertion {
	jobNode := findGHJobValueNode(doc, v.Line, v.Col)
	if jobNode == nil || jobNode.Kind != yaml.MappingNode || len(jobNode.Content) == 0 {
		return nil
	}
	indent := jobNode.Content[0].Column - 1
	return &insertion{after: v.Line, text: strings.Repeat(" ", indent) + line}
}

func fixAzureJobKey(doc *yaml.Node, v rules.Violation, line string) *insertion {
	jobNode := azureparser.FindJobNode(doc, v.Line, v.Col)
	if jobNode == nil || jobNode.Kind != yaml.MappingNode || len(jobNode.Content) == 0 {
		return nil
	}
	indent := jobNode.Content[0].Column - 1
	return &insertion{after: v.Line, text: strings.Repeat(" ", indent) + line}
}

// fixGHStepWithKey adds key: value to a step's "with:" block, creating
// the block if it doesn't exist yet. Bails out (returns nil, leaving the
// violation unfixed) if an existing "with:" isn't a plain block mapping
// — flow style ("with: {ref: main}") or an empty mapping node aren't
// worth the risk of a subtly wrong insertion.
func fixGHStepWithKey(doc *yaml.Node, v rules.Violation, key, value string) *insertion {
	stepNode := findGHStepNode(doc, v.Line, v.Col)
	if stepNode == nil || stepNode.Kind != yaml.MappingNode || len(stepNode.Content) == 0 {
		return nil
	}
	if withNode := mapValue(stepNode, "with"); withNode != nil {
		if withNode.Kind != yaml.MappingNode || withNode.Style == yaml.FlowStyle || len(withNode.Content) == 0 {
			return nil
		}
		indent := withNode.Content[0].Column - 1
		return &insertion{after: withNode.Line, text: strings.Repeat(" ", indent) + key + ": " + value}
	}
	indent := stepNode.Content[0].Column - 1
	return &insertion{
		after: v.Line,
		text: strings.Repeat(" ", indent) + "with:\n" +
			strings.Repeat(" ", indent+2) + key + ": " + value,
	}
}

func fixGHConcurrency(doc *yaml.Node) *insertion {
	jobsKey := findKeyNode(doc, "jobs")
	if jobsKey == nil {
		return nil
	}
	return &insertion{
		after: jobsKey.Line - 1,
		text: "concurrency:\n" +
			"  group: ${{ github.workflow }}-${{ github.ref }}\n" +
			"  cancel-in-progress: true",
	}
}

func findGHJobValueNode(doc *yaml.Node, line, col int) *yaml.Node {
	jobsNode := mapValue(doc, "jobs")
	if jobsNode == nil || jobsNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(jobsNode.Content); i += 2 {
		if jobsNode.Content[i].Line == line && jobsNode.Content[i].Column == col {
			return jobsNode.Content[i+1]
		}
	}
	return nil
}

func findGHStepNode(doc *yaml.Node, line, col int) *yaml.Node {
	jobsNode := mapValue(doc, "jobs")
	if jobsNode == nil || jobsNode.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(jobsNode.Content); i += 2 {
		jobNode := jobsNode.Content[i+1]
		stepsNode := mapValue(jobNode, "steps")
		if stepsNode == nil || stepsNode.Kind != yaml.SequenceNode {
			continue
		}
		for _, stepNode := range stepsNode.Content {
			if stepNode.Line == line && stepNode.Column == col {
				return stepNode
			}
		}
	}
	return nil
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func findKeyNode(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i]
		}
	}
	return nil
}

// applyInsertions splices every insertion into raw's lines. Applied in
// descending line order so an earlier insertion's target line number
// never shifts out from under it because of one applied after it.
func applyInsertions(raw []byte, insertions []insertion) []byte {
	sort.SliceStable(insertions, func(i, j int) bool { return insertions[i].after > insertions[j].after })

	lines := strings.Split(string(raw), "\n")
	for _, ins := range insertions {
		at := ins.after
		if at < 0 {
			at = 0
		}
		if at > len(lines) {
			at = len(lines)
		}
		newLines := strings.Split(ins.text, "\n")
		spliced := make([]string, 0, len(lines)+len(newLines))
		spliced = append(spliced, lines[:at]...)
		spliced = append(spliced, newLines...)
		spliced = append(spliced, lines[at:]...)
		lines = spliced
	}
	return []byte(strings.Join(lines, "\n"))
}
