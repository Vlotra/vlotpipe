// Lean-pipeline rules: not about safety, but about pipeline files staying
// short and build times staying short. See docs/LEAN_PIPELINES_RESEARCH.md
// for the research behind this category.
package baseline

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(aptInstallRuntime{})
	rules.Register(unnecessaryFullClone{})
	rules.Register(missingDockerBuildCache{})
	rules.Register(missingArtifactRetention{})
	rules.Register(duplicateStepPrefix{})
}

var packageManagerInstall = regexp.MustCompile(`\b(apt-get|apt|apk|yum|dnf)\s+(install|add)\b`)

// languageRuntimeKeywords are package names that indicate a job is
// installing an entire language runtime/compiler from a distro package
// manager, one line at a time, instead of using a maintained setup-*
// action (which brings its own caching) or a container image that
// already has the toolchain baked in.
var languageRuntimeKeywords = []string{
	"python3", "python2", " python ",
	"nodejs", "node-", "npm ",
	"golang-go", "golang",
	"openjdk", "default-jdk", "default-jre",
	"ruby-full", "ruby-dev",
}

// LEAN001: reinstalling a language runtime from scratch on every run is
// both slow (no caching, full package-manager resolution each time) and
// verbose. A maintained actions/setup-* (with its cache: input) or a
// container: image with the toolchain preinstalled does the same job in
// a fraction of the time and a fraction of the YAML.
type aptInstallRuntime struct{}

func (aptInstallRuntime) Code() string             { return "LEAN001" }
func (aptInstallRuntime) Name() string             { return "apt-install-runtime" }
func (aptInstallRuntime) Severity() rules.Severity { return rules.SeverityWarning }
func (aptInstallRuntime) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions, model.PlatformAzurePipelines}
}

func (aptInstallRuntime) Check(p *model.Pipeline) []rules.Violation {
	toolInstallerHint := "use the matching actions/setup-* action (with caching) or a container: image that already has it"
	if p.Platform == model.PlatformAzurePipelines {
		toolInstallerHint = "use the matching Tool Installer task (e.g. UsePythonVersion@0) or a container: image that already has it"
	}
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			run := strings.ToLower(step.Run)
			if !packageManagerInstall.MatchString(run) {
				continue
			}
			for _, kw := range languageRuntimeKeywords {
				if !strings.Contains(run, kw) {
					continue
				}
				out = append(out, rules.Violation{
					Code:     "LEAN001",
					Rule:     "apt-install-runtime",
					Severity: rules.SeverityWarning,
					Message:  "installs a language runtime via the package manager on every run; " + toolInstallerHint,
					Path:     p.Path,
					Line:     step.Line,
					Col:      step.Col,
				})
				break
			}
		}
	}
	return out
}

var gitHistoryUseKeywords = []string{"describe", "changelog", "git log", "git blame", "shortlog"}

// LEAN002: actions/checkout already defaults to fetch-depth: 1. Explicitly
// requesting the full history (fetch-depth: 0) downloads every commit and
// blob the repo has ever had, which is slow on any repo with real age or
// size, and is usually only needed for changelog generation, git describe,
// or similar history-walking operations.
type unnecessaryFullClone struct{}

func (unnecessaryFullClone) Code() string             { return "LEAN002" }
func (unnecessaryFullClone) Name() string             { return "unnecessary-full-clone" }
func (unnecessaryFullClone) Severity() rules.Severity { return rules.SeverityInfo }
func (unnecessaryFullClone) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (unnecessaryFullClone) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			if step.With["fetch-depth"] != "0" {
				continue
			}
			if jobLooksLikeItNeedsHistory(job) {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "LEAN002",
				Rule:     "unnecessary-full-clone",
				Severity: rules.SeverityInfo,
				Message:  "fetch-depth: 0 clones the entire git history; only set it if a later step actually walks history (changelog, git describe, blame)",
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}

func jobLooksLikeItNeedsHistory(job model.Job) bool {
	for _, step := range job.Steps {
		run := strings.ToLower(step.Run)
		for _, kw := range gitHistoryUseKeywords {
			if strings.Contains(run, kw) {
				return true
			}
		}
	}
	return false
}

// LEAN010: without a cache backend, docker/build-push-action rebuilds
// every layer from scratch on every run — Docker's own build cache lives
// in the ephemeral runner's local disk, so it's gone before the next
// run even starts unless it's explicitly exported somewhere (GitHub
// Actions cache, a registry, or a remote builder) and re-imported.
type missingDockerBuildCache struct{}

func (missingDockerBuildCache) Code() string             { return "LEAN010" }
func (missingDockerBuildCache) Name() string             { return "missing-docker-build-cache" }
func (missingDockerBuildCache) Severity() rules.Severity { return rules.SeverityWarning }
func (missingDockerBuildCache) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (missingDockerBuildCache) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "docker/build-push-action@") {
				continue
			}
			_, hasFrom := step.With["cache-from"]
			_, hasTo := step.With["cache-to"]
			if hasFrom && hasTo {
				continue
			}
			if isCachedRunner("LEAN010", job.RunsOn) {
				continue
			}
			// Same reasoning as PERF001: the "every build starts from
			// zero" assumption only holds on an ephemeral runner. A
			// persistent self-hosted worker keeps Docker's local build
			// cache on disk between jobs even with no cache-from/cache-to
			// backend configured, so this downgrades confidence rather
			// than assuming the worst when the runner isn't recognizably
			// ephemeral.
			sev := rules.SeverityWarning
			msg := "docker/build-push-action has no cache-from/cache-to; every layer rebuilds from scratch each run instead of reusing a GitHub Actions (type=gha) or registry cache"
			if job.RunsOn != "" && !strings.Contains(job.RunsOn, "${{") && !looksLikeEphemeralRunner(job.RunsOn) {
				sev = rules.SeverityInfo
				msg = "docker/build-push-action has no cache-from/cache-to; low-confidence since '" + job.RunsOn + "' doesn't look like an ephemeral runner — Docker's local build cache may already persist on disk between runs on a self-hosted worker"
			}
			out = append(out, rules.Violation{
				Code:     "LEAN010",
				Rule:     "missing-docker-build-cache",
				Severity: sev,
				Message:  msg,
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}

// LEAN011: actions/upload-artifact defaults to a 90-day retention period.
// Most CI-only artifacts (test logs, coverage reports, intermediate
// build output consumed by a later job in the same run) are useless
// after the run finishes, so that default quietly accumulates storage
// for artifacts nobody will ever download again.
type missingArtifactRetention struct{}

func (missingArtifactRetention) Code() string             { return "LEAN011" }
func (missingArtifactRetention) Name() string             { return "missing-artifact-retention" }
func (missingArtifactRetention) Severity() rules.Severity { return rules.SeverityInfo }
func (missingArtifactRetention) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (missingArtifactRetention) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/upload-artifact@") {
				continue
			}
			if _, ok := step.With["retention-days"]; ok {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "LEAN011",
				Rule:     "missing-artifact-retention",
				Severity: rules.SeverityInfo,
				Message:  "actions/upload-artifact has no retention-days; defaults to 90 days even for artifacts only needed within this run",
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}

const duplicatePrefixLength = 3

// stepSignature identifies a step by what it actually does — the action
// it calls and how, or the script it runs — ignoring its display name,
// so two steps that do the same thing under different labels still
// count as duplicates.
func stepSignature(s model.Step) string {
	if s.Uses != "" {
		keys := make([]string, 0, len(s.With))
		for k := range s.With {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var with strings.Builder
		for _, k := range keys {
			with.WriteString(k)
			with.WriteByte('=')
			with.WriteString(s.With[k])
			with.WriteByte(';')
		}
		return "uses:" + s.Uses + "|" + with.String()
	}
	return "run:" + strings.TrimSpace(s.Run)
}

// jobPrefixSignature returns the signature of a job's first
// duplicatePrefixLength steps, joined, or "" if the job has fewer.
func jobPrefixSignature(job model.Job) string {
	if len(job.Steps) < duplicatePrefixLength {
		return ""
	}
	sigs := make([]string, duplicatePrefixLength)
	for i := 0; i < duplicatePrefixLength; i++ {
		sigs[i] = stepSignature(job.Steps[i])
	}
	return strings.Join(sigs, "\x00")
}

// LEAN008: two or more jobs in the same file that open with the same
// steps — almost always checkout + setup + cache, copy-pasted rather
// than extracted — are exactly what composite actions and reusable
// workflows exist to name once instead of repeating.
type duplicateStepPrefix struct{}

func (duplicateStepPrefix) Code() string             { return "LEAN008" }
func (duplicateStepPrefix) Name() string             { return "duplicate-step-prefix" }
func (duplicateStepPrefix) Severity() rules.Severity { return rules.SeverityInfo }
func (duplicateStepPrefix) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions, model.PlatformAzurePipelines}
}

func (duplicateStepPrefix) Check(p *model.Pipeline) []rules.Violation {
	firstJobWithSig := map[string]model.Job{}
	var out []rules.Violation
	for _, job := range p.Jobs {
		sig := jobPrefixSignature(job)
		if sig == "" {
			continue
		}
		first, seen := firstJobWithSig[sig]
		if !seen {
			firstJobWithSig[sig] = job
			continue
		}
		out = append(out, rules.Violation{
			Code:     "LEAN008",
			Rule:     "duplicate-step-prefix",
			Severity: rules.SeverityInfo,
			Message:  fmt.Sprintf("job '%s' repeats the same first %d steps as job '%s'; %s would say it once", job.ID, duplicatePrefixLength, first.ID, reusableUnitName(p.Platform)),
			Path:     p.Path,
			Line:     job.Line,
			Col:      job.Col,
		})
	}
	return out
}
