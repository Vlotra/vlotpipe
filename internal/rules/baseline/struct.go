package baseline

import (
	"fmt"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(missingTestJob{})
}

// STRUCT001: the pipeline has no job that looks like it runs tests or
// linting. Heuristic and low-confidence by design, hence info severity.
// Platform-neutral: nothing in the check or message is GitHub- or
// Azure-specific, so it applies to both without change.
type missingTestJob struct{}

func (missingTestJob) Code() string             { return "STRUCT001" }
func (missingTestJob) Name() string             { return "missing-test-job" }
func (missingTestJob) Severity() rules.Severity { return rules.SeverityInfo }
func (missingTestJob) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions, model.PlatformAzurePipelines}
}

var structHints = []string{"test", "lint", "check", "verify", "ci"}

func (missingTestJob) Check(p *model.Pipeline) []rules.Violation {
	if len(p.Jobs) == 0 {
		return nil
	}
	for _, job := range p.Jobs {
		id := strings.ToLower(job.ID)
		name := strings.ToLower(job.Name)
		for _, hint := range structHints {
			if strings.Contains(id, hint) || strings.Contains(name, hint) {
				return nil
			}
		}
	}
	first := p.Jobs[0]
	return []rules.Violation{{
		Code:     "STRUCT001",
		Rule:     "missing-test-job",
		Severity: rules.SeverityInfo,
		Message:  "no job name suggests a test/lint/check step runs in this pipeline",
		Path:     p.Path,
		Line:     first.Line,
		Col:      first.Col,
	}}
}

// DefaultMaxStepsPerJob is STRUCT002's threshold when .vlotpipe.yml
// doesn't override it. Calibrated against real-world workflows vetted
// while building vlotpipe (see docs/VETTING_*.md): even astral-sh/ruff's
// most complex CI jobs top out at 15 steps, so 20 catches genuine bloat
// without flagging legitimately large, well-run jobs.
const DefaultMaxStepsPerJob = 20

// CheckMaxStepsPerJob is STRUCT002: not a rules.Rule (registered via the
// baseline init() pack like every other rule), because its threshold is
// configurable via .vlotpipe.yml's "max_steps_per_job" — it's called
// directly by the CLI with the resolved threshold, the same pattern
// internal/customrules and internal/rules/repolevel use for checks that
// need scan-time configuration.
//
// A job that keeps growing step by step, PR after PR, rarely gets
// reorganized on purpose — it just accretes. Past a certain size, a
// reusable unit (a composite action/reusable workflow on GitHub Actions,
// a template on Azure Pipelines) says the same thing in far less
// repetition, and gives the repeated parts a name.
func CheckMaxStepsPerJob(p *model.Pipeline, maxSteps int) []rules.Violation {
	if maxSteps <= 0 {
		maxSteps = DefaultMaxStepsPerJob
	}
	var out []rules.Violation
	for _, job := range p.Jobs {
		if len(job.Steps) <= maxSteps {
			continue
		}
		out = append(out, rules.Violation{
			Code:     "STRUCT002",
			Rule:     "job-too-many-steps",
			Severity: rules.SeverityInfo,
			Message:  fmt.Sprintf("job '%s' has %d steps (over %d); consider %s for the repeated parts", job.ID, len(job.Steps), maxSteps, reusableUnitName(p.Platform)),
			Path:     p.Path,
			Line:     job.Line,
			Col:      job.Col,
		})
	}
	return out
}

// reusableUnitName names the platform-appropriate mechanism for
// factoring out repeated step sequences, for use in STRUCT002 and
// LEAN008's messages.
func reusableUnitName(platform model.Platform) string {
	if platform == model.PlatformAzurePipelines {
		return "a template"
	}
	return "a composite action or reusable workflow"
}
