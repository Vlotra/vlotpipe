package baseline

import (
	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(missingTimeout{})
}

// TIMEOUT001: a job with no timeout-minutes inherits GitHub's 360-minute
// default, so a hung step (a stuck prompt, a runaway process) burns runner
// minutes for six hours before it's killed.
type missingTimeout struct{}

func (missingTimeout) Code() string             { return "TIMEOUT001" }
func (missingTimeout) Name() string             { return "missing-timeout" }
func (missingTimeout) Severity() rules.Severity { return rules.SeverityWarning }
func (missingTimeout) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (missingTimeout) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		if job.TimeoutMinutes > 0 {
			continue
		}
		out = append(out, rules.Violation{
			Code:     "TIMEOUT001",
			Rule:     "missing-timeout",
			Severity: rules.SeverityWarning,
			Message:  "job '" + job.ID + "' has no timeout-minutes; defaults to GitHub's 360-minute cap",
			Path:     p.Path,
			Line:     job.Line,
			Col:      job.Col,
		})
	}
	return out
}
