package baseline

import (
	"fmt"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(missingPersistCredentialsFalse{})
	rules.Register(insecureCommands{})
	rules.Register(hardcodedContainerCredentials{})
	rules.Register(releaseCachePoisoning{})
}

// SEC006: actions/checkout persists a git credential to disk by default so
// later steps can push. If a job never needs to push, that credential
// sitting on disk is an unnecessary way for it to leak (e.g. via an
// artifact upload of the whole workspace).
//
// Only the silent default is flagged: an explicit "persist-credentials:
// true" is a deliberate, documented opt-in (e.g. a job that pushes a
// generated commit), and GitHub's own guidance is to make that opt-in
// explicit rather than to avoid it — flagging it too would punish the
// exact behavior being recommended. Confirmed via vetting against
// astral-sh/ruff's publish-docs.yml and sync_typeshed.yaml, both of
// which set persist-credentials: true because the job pushes.
type missingPersistCredentialsFalse struct{}

func (missingPersistCredentialsFalse) Code() string { return "SEC006" }
func (missingPersistCredentialsFalse) Name() string {
	return "missing-persist-credentials-false"
}
func (missingPersistCredentialsFalse) Severity() rules.Severity { return rules.SeverityWarning }
func (missingPersistCredentialsFalse) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (missingPersistCredentialsFalse) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			if v, ok := step.With["persist-credentials"]; ok && (v == "false" || v == "true") {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "SEC006",
				Rule:     "missing-persist-credentials-false",
				Severity: rules.SeverityWarning,
				Message:  "actions/checkout persists a git credential to disk by default; set persist-credentials: false unless this job needs to push",
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}

const unsecureCommandsEnvVar = "ACTIONS_ALLOW_UNSECURE_COMMANDS"

func isTruthy(v string) bool {
	switch strings.ToLower(v) {
	case "", "false", "0", "no":
		return false
	default:
		return true
	}
}

// SEC013: GitHub deprecated ::set-env and ::add-path in 2020 because any
// step that can write to stdout could use them to inject environment
// variables or PATH entries into later steps. ACTIONS_ALLOW_UNSECURE_COMMANDS
// re-enables that behavior.
type insecureCommands struct{}

func (insecureCommands) Code() string             { return "SEC013" }
func (insecureCommands) Name() string             { return "insecure-commands" }
func (insecureCommands) Severity() rules.Severity { return rules.SeverityWarning }
func (insecureCommands) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (insecureCommands) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			v, ok := step.Env[unsecureCommandsEnvVar]
			if !ok || !isTruthy(v) {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "SEC013",
				Rule:     "insecure-commands",
				Severity: rules.SeverityWarning,
				Message:  "ACTIONS_ALLOW_UNSECURE_COMMANDS re-enables deprecated, injectable workflow commands (::set-env, ::add-path); use GITHUB_ENV/GITHUB_PATH files instead",
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}

// SEC014: hardcoded container/service registry credentials are as exposed
// as any other hardcoded secret — anyone with read access to the repo can
// read them straight out of the workflow file.
type hardcodedContainerCredentials struct{}

func (hardcodedContainerCredentials) Code() string { return "SEC014" }
func (hardcodedContainerCredentials) Name() string {
	return "hardcoded-container-credentials"
}
func (hardcodedContainerCredentials) Severity() rules.Severity { return rules.SeverityBlocker }
func (hardcodedContainerCredentials) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (hardcodedContainerCredentials) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	flag := func(c *model.Container, jobID string) {
		if c == nil || c.Password == "" || strings.Contains(c.Password, "${{") {
			return
		}
		out = append(out, rules.Violation{
			Code:     "SEC014",
			Rule:     "hardcoded-container-credentials",
			Severity: rules.SeverityBlocker,
			Message:  fmt.Sprintf("job '%s' has a hardcoded container registry password; use secrets.<NAME> instead", jobID),
			Path:     p.Path,
			Line:     c.Line,
			Col:      c.Col,
		})
	}
	for _, job := range p.Jobs {
		flag(job.Container, job.ID)
		for _, svc := range job.Services {
			flag(svc, job.ID)
		}
	}
	return out
}

// SEC015: release/publish workflows that restore GitHub Actions caches can
// have those caches poisoned by an attacker with any valid GITHUB_TOKEN
// (e.g. from an unrelated PR run), letting them run code in the privileged
// release job and potentially tamper with the published artifact.
type releaseCachePoisoning struct{}

func (releaseCachePoisoning) Code() string             { return "SEC015" }
func (releaseCachePoisoning) Name() string             { return "release-cache-poisoning" }
func (releaseCachePoisoning) Severity() rules.Severity { return rules.SeverityWarning }
func (releaseCachePoisoning) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (releaseCachePoisoning) Check(p *model.Pipeline) []rules.Violation {
	if !p.HasTrigger("release") {
		return nil
	}
	var out []rules.Violation
	for _, job := range p.Jobs {
		if !hasCaching(job) {
			continue
		}
		out = append(out, rules.Violation{
			Code:     "SEC015",
			Rule:     "release-cache-poisoning",
			Severity: rules.SeverityWarning,
			Message:  fmt.Sprintf("job '%s' restores a GitHub Actions cache in a release-triggered workflow; a poisoned cache entry can compromise the published artifact", job.ID),
			Path:     p.Path,
			Line:     job.Line,
			Col:      job.Col,
		})
	}
	return out
}
