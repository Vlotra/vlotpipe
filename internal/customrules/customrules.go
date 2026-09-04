// Package customrules evaluates the org-defined, no-Go-required policy
// checks declared under "custom_rules:" in .vlotpipe.yml. This is the
// escape hatch for company-specific policy that doesn't fit vlotpipe's
// baseline rule pack — e.g. "no job may set timeout-minutes to GitHub's
// own 360 default," which technically satisfies TIMEOUT001's "is a
// timeout set at all" check while missing the point of setting one.
package customrules

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/vlotra/vlotpipe/internal/config"
	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

// Rule is a compiled, ready-to-evaluate custom rule.
type Rule struct {
	Code     string
	Message  string
	Scope    string // "job" or "step"
	Field    string
	Severity rules.Severity

	equals    *string
	notEquals *string
	matches   *regexp.Regexp
	exists    *bool
}

// jobFields and stepFields are the field names a custom rule may select
// on, deliberately curated rather than exposed via reflection — this is
// the contract .vlotpipe.yml authors write against, so it should be
// small, stable, and explicit about what it does and doesn't cover.
var jobFields = map[string]bool{
	"timeout-minutes": true, "runs-on": true, "name": true,
	"id": true, "if": true, "permissions-set": true,
}

var stepFields = map[string]bool{
	"uses": true, "run": true, "name": true, "if": true,
}

// Compile validates and compiles config.CustomRule specs, failing loudly
// on the first invalid entry (bad severity, unknown field, no matcher at
// all) rather than silently matching nothing at scan time.
func Compile(specs []config.CustomRule) ([]Rule, error) {
	out := make([]Rule, 0, len(specs))
	for _, s := range specs {
		r, err := compileOne(s)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func compileOne(s config.CustomRule) (Rule, error) {
	if s.Code == "" {
		return Rule{}, fmt.Errorf("custom_rules: entry missing code")
	}
	sev := rules.Severity(s.Severity)
	switch sev {
	case rules.SeverityBlocker, rules.SeverityWarning, rules.SeverityInfo:
	default:
		return Rule{}, fmt.Errorf("custom_rules[%s]: invalid severity %q (want blocker, warning, or info)", s.Code, s.Severity)
	}

	scope := s.Scope
	if scope == "" {
		scope = "job"
	}
	fields := jobFields
	if scope == "step" {
		fields = stepFields
	} else if scope != "job" {
		return Rule{}, fmt.Errorf("custom_rules[%s]: invalid scope %q (want job or step)", s.Code, s.Scope)
	}
	if !fields[s.Field] {
		return Rule{}, fmt.Errorf("custom_rules[%s]: unsupported field %q for scope %q", s.Code, s.Field, scope)
	}

	r := Rule{
		Code: s.Code, Message: s.Message, Scope: scope, Field: s.Field,
		Severity: sev, equals: s.Equals, notEquals: s.NotEquals, exists: s.Exists,
	}
	if s.Matches != nil {
		re, err := regexp.Compile(*s.Matches)
		if err != nil {
			return Rule{}, fmt.Errorf("custom_rules[%s]: invalid matches regex: %w", s.Code, err)
		}
		r.matches = re
	}
	if r.equals == nil && r.notEquals == nil && r.matches == nil && r.exists == nil {
		return Rule{}, fmt.Errorf("custom_rules[%s]: needs at least one of equals, not_equals, matches, exists", s.Code)
	}
	return r, nil
}

// evaluate reports whether (value, exists) satisfies every matcher set
// on r. Matchers combine with AND: a rule with both "exists: true" and
// "equals: x" requires the field to be both present and equal to x.
func (r Rule) evaluate(value string, exists bool) bool {
	if r.exists != nil && exists != *r.exists {
		return false
	}
	if r.equals != nil && value != *r.equals {
		return false
	}
	if r.notEquals != nil && value == *r.notEquals {
		return false
	}
	if r.matches != nil && !r.matches.MatchString(value) {
		return false
	}
	return true
}

func jobFieldValue(job model.Job, field string) (value string, exists bool) {
	switch field {
	case "timeout-minutes":
		if job.TimeoutMinutes == 0 {
			return "", false
		}
		return strconv.Itoa(job.TimeoutMinutes), true
	case "runs-on":
		return job.RunsOn, job.RunsOn != ""
	case "name":
		return job.Name, job.Name != ""
	case "id":
		return job.ID, job.ID != ""
	case "if":
		return job.If, job.If != ""
	case "permissions-set":
		return strconv.FormatBool(job.PermissionsSet), true
	default:
		return "", false
	}
}

func stepFieldValue(step model.Step, field string) (value string, exists bool) {
	switch field {
	case "uses":
		return step.Uses, step.Uses != ""
	case "run":
		return step.Run, step.Run != ""
	case "name":
		return step.Name, step.Name != ""
	case "if":
		return step.If, step.If != ""
	default:
		return "", false
	}
}

// Check runs every compiled custom rule against p, in the same job/step
// scope each rule declared.
func Check(p *model.Pipeline, ruleset []Rule) []rules.Violation {
	var out []rules.Violation
	for _, r := range ruleset {
		if r.Scope == "step" {
			for _, job := range p.Jobs {
				for _, step := range job.Steps {
					value, exists := stepFieldValue(step, r.Field)
					if !r.evaluate(value, exists) {
						continue
					}
					out = append(out, violation(r, p.Path, step.Line, step.Col))
				}
			}
			continue
		}
		for _, job := range p.Jobs {
			value, exists := jobFieldValue(job, r.Field)
			if !r.evaluate(value, exists) {
				continue
			}
			out = append(out, violation(r, p.Path, job.Line, job.Col))
		}
	}
	return out
}

func violation(r Rule, path string, line, col int) rules.Violation {
	return rules.Violation{
		Code:     r.Code,
		Rule:     r.Code,
		Severity: r.Severity,
		Message:  r.Message,
		Path:     path,
		Line:     line,
		Col:      col,
	}
}
