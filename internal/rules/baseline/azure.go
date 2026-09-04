// Azure Pipelines-specific rules. Not every GitHub Actions rule has an
// Azure equivalent — see docs/AZURE_RESEARCH.md for which concepts do
// and don't carry over, and why. These two don't: Azure's task
// versioning has no SHA-pin equivalent (SEC001), and its checkout step
// already defaults to not persisting credentials (SEC006's opposite of
// GitHub's default), so neither ports.
package baseline

import (
	"fmt"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(azureMissingTimeout{})
	rules.Register(azureHardcodedSecret{})
}

// AZR001: a job with no timeoutInMinutes inherits Azure's 60-minute
// default on Microsoft-hosted agents (unlimited on self-hosted) — a
// different number from GitHub Actions' 360-minute default (TIMEOUT001),
// so this is its own rule rather than a shared message.
type azureMissingTimeout struct{}

func (azureMissingTimeout) Code() string             { return "AZR001" }
func (azureMissingTimeout) Name() string             { return "azure-missing-timeout" }
func (azureMissingTimeout) Severity() rules.Severity { return rules.SeverityWarning }
func (azureMissingTimeout) Platforms() []model.Platform {
	return []model.Platform{model.PlatformAzurePipelines}
}

func (azureMissingTimeout) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		if job.TimeoutMinutes > 0 {
			continue
		}
		out = append(out, rules.Violation{
			Code:     "AZR001",
			Rule:     "azure-missing-timeout",
			Severity: rules.SeverityWarning,
			Message:  fmt.Sprintf("job '%s' has no timeoutInMinutes; defaults to 60 minutes on Microsoft-hosted agents (unlimited on self-hosted)", job.ID),
			Path:     p.Path,
			Line:     job.Line,
			Col:      job.Col,
		})
	}
	return out
}

// AZR002: a task's "inputs:"/"env:" value that looks like a credential
// but isn't a "$(variableName)" reference into an Azure Pipelines
// variable/variable group/Key Vault-linked secret. Same shape of
// mistake as SEC002, expressed with Azure's own variable syntax
// instead of GitHub's "${{ secrets.* }}".
type azureHardcodedSecret struct{}

func (azureHardcodedSecret) Code() string             { return "AZR002" }
func (azureHardcodedSecret) Name() string             { return "azure-hardcoded-secret" }
func (azureHardcodedSecret) Severity() rules.Severity { return rules.SeverityBlocker }
func (azureHardcodedSecret) Platforms() []model.Platform {
	return []model.Platform{model.PlatformAzurePipelines}
}

func (azureHardcodedSecret) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	check := func(step model.Step, m map[string]string) {
		for k, v := range m {
			if v == "" || !secretKey.MatchString(k) {
				continue
			}
			if strings.Contains(v, "$(") {
				continue // references a pipeline variable
			}
			out = append(out, rules.Violation{
				Code:     "AZR002",
				Rule:     "azure-hardcoded-secret",
				Severity: rules.SeverityBlocker,
				Message:  fmt.Sprintf("%q looks like a hardcoded credential; reference a pipeline variable with $(%s) instead", k, k),
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
