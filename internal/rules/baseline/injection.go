package baseline

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(templateInjection{})
	rules.Register(spoofableBotCondition{})
}

// SEC004: template expansion happens before the shell ever runs, so
// interpolating attacker-controlled input (a PR/issue title, a comment
// body, ...) straight into "run:" lets that input become shell syntax
// rather than a quoted string — the standard GitHub Actions script
// injection vector.
type templateInjection struct{}

func (templateInjection) Code() string             { return "SEC004" }
func (templateInjection) Name() string             { return "template-injection" }
func (templateInjection) Severity() rules.Severity { return rules.SeverityBlocker }
func (templateInjection) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (templateInjection) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			exprs := findUntrustedExprs(step.Run)
			if len(exprs) == 0 {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "SEC004",
				Rule:     "template-injection",
				Severity: rules.SeverityBlocker,
				Message:  fmt.Sprintf("untrusted input %s is interpolated directly into run:; pass it through an intermediate env var instead", strings.Join(exprs, ", ")),
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}

var actorEqualityPattern = regexp.MustCompile(`github\.actor\s*==`)

// SEC011: github.actor reflects the last actor to act on the triggering
// context, not necessarily the PR's author — an attacker can get a
// trusted bot's name onto the HEAD commit while attacker-controlled code
// sits earlier in the branch history, bypassing an actor-only check.
type spoofableBotCondition struct{}

func (spoofableBotCondition) Code() string             { return "SEC011" }
func (spoofableBotCondition) Name() string             { return "spoofable-bot-condition" }
func (spoofableBotCondition) Severity() rules.Severity { return rules.SeverityWarning }
func (spoofableBotCondition) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (spoofableBotCondition) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	flag := func(cond string, line, col int) {
		if !actorEqualityPattern.MatchString(cond) {
			return
		}
		out = append(out, rules.Violation{
			Code:     "SEC011",
			Rule:     "spoofable-bot-condition",
			Severity: rules.SeverityWarning,
			Message:  "github.actor is spoofable for authorization checks; use github.event.pull_request.user.login instead",
			Path:     p.Path,
			Line:     line,
			Col:      col,
		})
	}
	for _, job := range p.Jobs {
		flag(job.If, job.Line, job.Col)
		for _, step := range job.Steps {
			flag(step.If, step.Line, step.Col)
		}
	}
	return out
}
