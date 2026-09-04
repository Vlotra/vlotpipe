package baseline

import (
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(missingCache{})
	rules.Register(missingConcurrency{})
	rules.Register(superfluousAction{})
}

// PERF001: a job installs dependencies with a package manager but never
// caches them, so every run re-downloads the same packages from scratch.
type missingCache struct{}

func (missingCache) Code() string             { return "PERF001" }
func (missingCache) Name() string             { return "missing-dependency-cache" }
func (missingCache) Severity() rules.Severity { return rules.SeverityWarning }
func (missingCache) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions, model.PlatformAzurePipelines}
}

var installCommands = []string{
	"npm ci", "npm install", "yarn install", "pnpm install",
	"pip install", "poetry install", "bundle install", "go mod download",
}

func (missingCache) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		if !installsDeps(job) || hasCaching(job) {
			continue
		}
		if isCachedRunner("PERF001", job.RunsOn) {
			continue
		}
		// The "every run starts from nothing" assumption behind this rule
		// only holds on an ephemeral runner. A self-hosted worker is often
		// a persistent, reused machine, where the previous job may have
		// already left the dependency cache sitting on local disk — the
		// absence of an explicit actions/cache step doesn't necessarily
		// mean the absence of a cache. We can't tell for certain from the
		// YAML alone (a self-hosted pool can itself be ephemeral, e.g.
		// autoscaled per job), so this downgrades confidence rather than
		// suppressing outright.
		cacheHint := "actions/cache or setup-*'s cache: input"
		if p.Platform == model.PlatformAzurePipelines {
			cacheHint = "a Cache@2 task"
		}
		sev := rules.SeverityWarning
		msg := "job '" + job.ID + "' installs dependencies without caching them (" + cacheHint + ")"
		if job.RunsOn != "" && !strings.Contains(job.RunsOn, "${{") && !looksLikeEphemeralRunner(job.RunsOn) {
			sev = rules.SeverityInfo
			msg = "job '" + job.ID + "' installs dependencies without caching them (" + cacheHint + "); low-confidence since '" + job.RunsOn + "' doesn't look like an ephemeral runner — if it's a persistent self-hosted worker, dependencies may already be cached on disk from a prior run"
		}
		out = append(out, rules.Violation{
			Code:     "PERF001",
			Rule:     "missing-dependency-cache",
			Severity: sev,
			Message:  msg,
			Path:     p.Path,
			Line:     job.Line,
			Col:      job.Col,
		})
	}
	return out
}

func installsDeps(job model.Job) bool {
	for _, step := range job.Steps {
		run := strings.ToLower(step.Run)
		for _, cmd := range installCommands {
			if strings.Contains(run, cmd) {
				return true
			}
		}
	}
	return false
}

func hasCaching(job model.Job) bool {
	for _, step := range job.Steps {
		if strings.HasPrefix(step.Uses, "actions/cache@") || strings.HasPrefix(step.Uses, "actions/cache/") {
			return true
		}
		if strings.HasPrefix(step.Uses, "actions/setup-") {
			if v, ok := step.With["cache"]; ok && v != "" && v != "false" {
				return true
			}
		}
		if strings.HasPrefix(step.Uses, "Cache@") {
			return true
		}
	}
	return false
}

// PERF002: without a concurrency group, a new push to the same PR/branch
// doesn't cancel the now-superseded run of a previous push — both keep
// occupying runners and billed minutes until they finish.
type missingConcurrency struct{}

func (missingConcurrency) Code() string             { return "PERF002" }
func (missingConcurrency) Name() string             { return "missing-concurrency-group" }
func (missingConcurrency) Severity() rules.Severity { return rules.SeverityInfo }
func (missingConcurrency) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (missingConcurrency) Check(p *model.Pipeline) []rules.Violation {
	if !p.HasTrigger("pull_request") || p.Concurrency {
		return nil
	}
	return []rules.Violation{{
		Code:     "PERF002",
		Rule:     "missing-concurrency-group",
		Severity: rules.SeverityInfo,
		Message:  "no concurrency: group with cancel-in-progress; superseded pull_request runs keep occupying runners instead of being cancelled",
		Path:     p.Path,
		Line:     1,
		Col:      1,
	}}
}

// superfluousActions maps third-party actions that wrap a CLI already
// preinstalled on GitHub-hosted runners (or trivially available) to the
// command that replaces them, per zizmor's superfluous-actions catalog.
var superfluousActions = map[string]string{
	"ncipollo/release-action":              "gh release create",
	"softprops/action-gh-release":          "gh release create",
	"elgohr/Github-Release-Action":         "gh release create",
	"peter-evans/create-pull-request":      "gh pr create",
	"peter-evans/create-or-update-comment": "gh pr comment / gh issue comment",
	"dacbd/create-issue-action":            "gh issue create",
	"actions-ecosystem/action-add-labels":  "gh issue edit --add-label / gh pr edit --add-label",
	"svenstaro/upload-release-action":      "gh release create / gh release upload",
	"addnab/docker-run-action":             "docker run",
	"sergeysova/jq-action":                 "jq",
	"dtolnay/rust-toolchain":               "rustup",
	"stefanzweifel/git-auto-commit-action": "git add / git commit / git push",
	"EndBug/add-and-commit":                "git add / git commit / git push",
}

// PERF003: these actions perform an operation the runner image or a
// preinstalled CLI already provides directly, at the cost of an extra
// third-party dependency in the critical path.
type superfluousAction struct{}

func (superfluousAction) Code() string             { return "PERF003" }
func (superfluousAction) Name() string             { return "superfluous-action" }
func (superfluousAction) Severity() rules.Severity { return rules.SeverityInfo }
func (superfluousAction) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (superfluousAction) Check(p *model.Pipeline) []rules.Violation {
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			at := strings.Index(step.Uses, "@")
			if at < 0 {
				continue
			}
			slug := step.Uses[:at]
			replacement, ok := superfluousActions[slug]
			if !ok {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "PERF003",
				Rule:     "superfluous-action",
				Severity: rules.SeverityInfo,
				Message:  "'" + slug + "' wraps functionality already available via '" + replacement + "'; using it directly removes a third-party dependency from the critical path",
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}
