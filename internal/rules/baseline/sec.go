package baseline

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(unpinnedAction{})
	rules.Register(hardcodedSecret{})
}

var fullSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// SEC001: actions must be pinned to a full commit SHA, not a mutable tag
// like @v4 or @main. Tags can be moved by the action's author (or an
// attacker who compromises their account) to point at different code
// without changing the ref in your workflow.
type unpinnedAction struct{}

func (unpinnedAction) Code() string             { return "SEC001" }
func (unpinnedAction) Name() string             { return "unpinned-action" }
func (unpinnedAction) Severity() rules.Severity { return rules.SeverityBlocker }
func (unpinnedAction) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (unpinnedAction) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if step.Uses == "" || strings.HasPrefix(step.Uses, "./") || strings.HasPrefix(step.Uses, "docker://") {
				continue
			}
			at := strings.LastIndex(step.Uses, "@")
			if at < 0 {
				continue
			}
			ref := step.Uses[at+1:]
			if fullSHA.MatchString(ref) {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "SEC001",
				Rule:     "unpinned-action",
				Severity: rules.SeverityBlocker,
				Message:  fmt.Sprintf("action %q is pinned to %q, not a full commit SHA", step.Uses[:at], ref),
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}

// SEC002: a with/env value looks like a hardcoded credential rather than a
// reference into GitHub's encrypted secrets/vars context.
type hardcodedSecret struct{}

func (hardcodedSecret) Code() string             { return "SEC002" }
func (hardcodedSecret) Name() string             { return "hardcoded-secret" }
func (hardcodedSecret) Severity() rules.Severity { return rules.SeverityBlocker }
func (hardcodedSecret) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

var secretKey = regexp.MustCompile(`(?i)(password|secret|token|api[_-]?key|access[_-]?key)`)

func (hardcodedSecret) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	check := func(step model.Step, m map[string]string) {
		for k, v := range m {
			if v == "" || !secretKey.MatchString(k) {
				continue
			}
			if strings.Contains(v, "${{") {
				continue // references an expression/secrets/vars context
			}
			out = append(out, rules.Violation{
				Code:     "SEC002",
				Rule:     "hardcoded-secret",
				Severity: rules.SeverityBlocker,
				Message:  fmt.Sprintf("%q looks like a hardcoded credential; use secrets.%s instead", k, strings.ToUpper(k)),
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			check(step, step.With)
			check(step, step.Env)
		}
	}
	return out
}
