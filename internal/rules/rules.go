// Package rules defines the rule engine: the Rule interface, Violation
// type, and a registry that baseline (and future third-party) rules
// register themselves into via init().
package rules

import (
	"sort"

	"github.com/vlotra/vlotpipe/internal/model"
)

// Severity ranks how urgently a violation should be addressed.
type Severity string

const (
	SeverityBlocker Severity = "blocker"
	SeverityWarning Severity = "warning"
	SeverityInfo    Severity = "info"
)

// weight is used to sort violations blocker-first and to compare against
// a --severity floor.
func (s Severity) weight() int {
	switch s {
	case SeverityBlocker:
		return 3
	case SeverityWarning:
		return 2
	default:
		return 1
	}
}

// AtLeast reports whether s is at least as severe as floor.
func (s Severity) AtLeast(floor Severity) bool {
	return s.weight() >= floor.weight()
}

// Violation is one finding, located precisely enough to jump to in an editor.
type Violation struct {
	Code     string   `json:"code"`
	Rule     string   `json:"rule"`
	Severity Severity `json:"severity"`
	Message  string   `json:"message"`
	Path     string   `json:"path"`
	Line     int      `json:"line"`
	Col      int      `json:"col"`
}

// Rule checks one policy against a parsed Pipeline.
type Rule interface {
	Code() string
	Name() string
	Severity() Severity
	Platforms() []model.Platform
	Check(p *model.Pipeline) []Violation
}

var registry []Rule

// Register adds a rule to the default rule set. Rule packages call this
// from init().
func Register(r Rule) {
	registry = append(registry, r)
}

// All returns every registered rule, sorted by code for stable output.
func All() []Rule {
	out := make([]Rule, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool { return out[i].Code() < out[j].Code() })
	return out
}

// appliesTo reports whether r runs against pipelines from the given platform.
func appliesTo(r Rule, platform model.Platform) bool {
	for _, p := range r.Platforms() {
		if p == platform {
			return true
		}
	}
	return false
}

// Run executes every registered rule applicable to p's platform and returns
// all violations, sorted by line then severity.
func Run(p *model.Pipeline, ignore func(code string, path string) bool) []Violation {
	var out []Violation
	for _, r := range All() {
		if !appliesTo(r, p.Platform) {
			continue
		}
		for _, v := range r.Check(p) {
			if ignore != nil && ignore(v.Code, v.Path) {
				continue
			}
			if p.IsSuppressed(v.Line, v.Code) {
				continue
			}
			out = append(out, v)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Line != out[j].Line {
			return out[i].Line < out[j].Line
		}
		return out[i].Code < out[j].Code
	})
	return out
}
