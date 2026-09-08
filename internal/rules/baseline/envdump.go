package baseline

import (
	"regexp"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(environmentDump{})
}

// envDumpPatterns match a shell/PowerShell command run bare (no arguments
// narrowing it to a single variable), which prints every environment
// variable visible to the step. Each is anchored so it must be a whole
// statement — "env" preceded by "get" (as in "getenv") or followed by
// more of a word (as in "envsubst") does not match, and "printenv FOO"
// (a single named variable, the safe form) does not either.
var envDumpPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?im)(^|[;&|]\s*)printenv(\s*$|\s*[;&|])`),
	regexp.MustCompile(`(?im)(^|[;&|]\s*)env(\s*$|\s*[;&|])`),
	regexp.MustCompile(`(?im)(^|[;&|]\s*)set(\s*$|\s*[;&|])`), // cmd.exe's bare "set"
	regexp.MustCompile(`(?im)(get-childitem|gci|dir|ls)\s+(-path\s+)?env:\s*$`),
	regexp.MustCompile(`(?i)\[environment\]::getenvironmentvariables\s*\(`),
}

// SEC016: a step that dumps the full environment (printenv/env with no
// arguments, cmd.exe's bare "set", PowerShell's "Get-ChildItem Env:")
// prints every variable visible to it straight into the build log —
// including any secret that isn't on the platform's known-secrets mask
// list, which happens more often than it should (a variable added to a
// group without the "secret" checkbox, a token passed through env: that
// the platform never learned to redact). Same reasoning as SEC008
// (toJSON(secrets)): dumping everything exposes more than the step
// needs, regardless of whether masking happens to catch it this time.
// Applies equally to GitHub Actions "run:" and Azure Pipelines
// "script:"/"bash:"/"powershell:"/"pwsh:" — the risk is in the shell
// command itself, not anything platform-specific. Found while vetting
// AvaloniaUI/Avalonia's azure-pipelines.yml,
// which runs "printenv" as a debug step on two of its four jobs.
type environmentDump struct{}

func (environmentDump) Code() string             { return "SEC016" }
func (environmentDump) Name() string             { return "environment-dump" }
func (environmentDump) Severity() rules.Severity { return rules.SeverityWarning }
func (environmentDump) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions, model.PlatformAzurePipelines}
}

func (environmentDump) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if step.Run == "" {
				continue
			}
			matched := false
			for _, re := range envDumpPatterns {
				if re.MatchString(step.Run) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "SEC016",
				Rule:     "environment-dump",
				Severity: rules.SeverityWarning,
				Message:  "this step dumps the entire environment to the log; any secret not on the platform's known-secrets mask list leaks in plaintext — print only the specific variable(s) needed",
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}
