package baseline

import (
	"fmt"
	"regexp"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(missingPermissions{})
	rules.Register(secretsInherit{})
	rules.Register(secretsDump{})
}

// SEC005: without an explicit "permissions:" block, GITHUB_TOKEN gets
// GitHub's default scope for the repo/org, which can be broad
// (read/write on contents, issues, PRs, etc.) — every job silently
// inherits whatever that default is. Declaring permissions, even as
// "permissions: {}", makes the actual scope explicit and auditable.
type missingPermissions struct{}

func (missingPermissions) Code() string             { return "SEC005" }
func (missingPermissions) Name() string             { return "missing-permissions" }
func (missingPermissions) Severity() rules.Severity { return rules.SeverityWarning }
func (missingPermissions) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (missingPermissions) Check(p *model.Pipeline) []rules.Violation {
	if p.PermissionsSet {
		return nil
	}
	var out []rules.Violation
	for _, job := range p.Jobs {
		if job.PermissionsSet {
			continue
		}
		out = append(out, rules.Violation{
			Code:     "SEC005",
			Rule:     "missing-permissions",
			Severity: rules.SeverityWarning,
			Message:  fmt.Sprintf("job '%s' has no permissions: set at the workflow or job level; GITHUB_TOKEN gets GitHub's default (possibly broad) scope", job.ID),
			Path:     p.Path,
			Line:     job.Line,
			Col:      job.Col,
		})
	}
	return out
}

// SEC007: "secrets: inherit" forwards every secret the calling workflow
// has access to into the reusable workflow, even ones it doesn't need,
// violating least privilege and making it impossible to audit which
// secrets a reusable workflow actually runs with.
type secretsInherit struct{}

func (secretsInherit) Code() string             { return "SEC007" }
func (secretsInherit) Name() string             { return "secrets-inherit" }
func (secretsInherit) Severity() rules.Severity { return rules.SeverityWarning }
func (secretsInherit) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (secretsInherit) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		if job.Uses == "" || !job.SecretsInherit {
			continue
		}
		out = append(out, rules.Violation{
			Code:     "SEC007",
			Rule:     "secrets-inherit",
			Severity: rules.SeverityWarning,
			Message:  fmt.Sprintf("job '%s' uses 'secrets: inherit' when calling a reusable workflow; forward only the specific secrets it needs", job.ID),
			Path:     p.Path,
			Line:     job.SecretsLine,
			Col:      job.SecretsCol,
		})
	}
	return out
}

var toJSONSecretsPattern = regexp.MustCompile(`(?i)toJSON\(\s*secrets\s*\)`)

// SEC008: dumping the entire secrets context (e.g. via toJSON(secrets))
// exposes every secret configured on the repo to the runner, even ones
// the job never actually needs.
type secretsDump struct{}

func (secretsDump) Code() string             { return "SEC008" }
func (secretsDump) Name() string             { return "secrets-dump" }
func (secretsDump) Severity() rules.Severity { return rules.SeverityWarning }
func (secretsDump) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (secretsDump) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	check := func(s string, line, col int) {
		if !toJSONSecretsPattern.MatchString(s) {
			return
		}
		out = append(out, rules.Violation{
			Code:     "SEC008",
			Rule:     "secrets-dump",
			Severity: rules.SeverityWarning,
			Message:  "toJSON(secrets) exposes every secret configured on the repo; reference each needed secret individually instead",
			Path:     p.Path,
			Line:     line,
			Col:      col,
		})
	}
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			check(step.Run, step.Line, step.Col)
			for _, v := range step.With {
				check(v, step.Line, step.Col)
			}
			for _, v := range step.Env {
				check(v, step.Line, step.Col)
			}
		}
	}
	return out
}
