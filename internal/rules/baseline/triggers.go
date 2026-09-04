package baseline

import (
	"fmt"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func init() {
	rules.Register(dangerousTriggerCheckout{})
	rules.Register(selfHostedForkRunner{})
	rules.Register(githubEnvInjection{})
}

// dangerousTriggers are events GitHub grants elevated privileges
// (GITHUB_TOKEN write access, repo secrets) to, but that anyone can
// invoke without needing write access to the repo — pull_request_target
// and workflow_run are the well-known pair, but issue_comment is just as
// dangerous: any GitHub account can comment on a public issue/PR, and
// ChatOps-style "/run-tests"-triggered workflows built on it often check
// out and execute the commenter's code. Found via vetting against
// vitejs/vite's ecosystem-ci-trigger.yml.
var dangerousTriggers = []string{"pull_request_target", "workflow_run", "issue_comment"}

func hasDangerousTrigger(p *model.Pipeline) bool {
	for _, t := range dangerousTriggers {
		if p.HasTrigger(t) {
			return true
		}
	}
	return false
}

// refIsUntrustedHead reports whether a checkout "ref:" value points at a
// pull request's (or triggering workflow_run's/issue_comment's) untrusted
// head, rather than the safe base ref that pull_request_target/
// workflow_run check out by default.
func refIsUntrustedHead(ref string) bool {
	for _, needle := range []string{
		"pull_request.head",
		"event.workflow_run",
		"github.head_ref",
		"event.issue.number", // ChatOps: checkout refs/pull/<issue number>/head
		"refs/pull/",
	} {
		if strings.Contains(ref, needle) {
			return true
		}
	}
	return false
}

// SEC003: pull_request_target/workflow_run triggers run with write access
// to GITHUB_TOKEN and repo secrets. Explicitly checking out the untrusted
// PR/workflow_run head in that context lets a PR author's build scripts
// (or a malicious commit) execute with those privileges — GitHub Security
// Lab's "pwn request" pattern, and the single most-exploited GitHub
// Actions misconfiguration.
type dangerousTriggerCheckout struct{}

func (dangerousTriggerCheckout) Code() string             { return "SEC003" }
func (dangerousTriggerCheckout) Name() string             { return "dangerous-trigger-checkout" }
func (dangerousTriggerCheckout) Severity() rules.Severity { return rules.SeverityBlocker }
func (dangerousTriggerCheckout) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (dangerousTriggerCheckout) Check(p *model.Pipeline) []rules.Violation {
	if !hasDangerousTrigger(p) {
		return nil
	}
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if !strings.HasPrefix(step.Uses, "actions/checkout@") {
				continue
			}
			ref, ok := step.With["ref"]
			if !ok || !refIsUntrustedHead(ref) {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "SEC003",
				Rule:     "dangerous-trigger-checkout",
				Severity: rules.SeverityBlocker,
				Message:  "checks out an untrusted PR/workflow_run head in a pull_request_target/workflow_run workflow, exposing GITHUB_TOKEN and secrets to attacker-controlled code",
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}

// hostedRunnerPrefixes covers both GitHub-hosted runner labels
// (ubuntu-*/windows-*/macos-*) and Azure Microsoft-hosted pool vmImage
// names, which use the same family of names — Azure's is capitalized
// "macOS-latest" rather than GitHub's "macos-latest", which is exactly
// why the match below is case-insensitive rather than a second,
// near-duplicate prefix list.
var hostedRunnerPrefixes = []string{"ubuntu-", "windows-", "macos-"}

// managedRunnerPrefixes are third-party runner-as-a-service providers
// (ephemeral, provider-managed cloud VMs, not a repo owner's own
// infrastructure). Their fork-PR risk profile is much closer to a
// hosted runner than to a literal self-hosted/self-managed one, so
// SEC010 (and, on the performance side, PERF001/LEAN010) treat them as
// safe rather than flagging every custom runner label. Found via
// vetting against astral-sh/ruff, which uses Depot and CodSpeed
// extensively and triggered false positives before this list existed.
var managedRunnerPrefixes = []string{
	"depot-", "buildjet-", "warp-", "codspeed", "namespace-", "blacksmith-", "ubicloud-",
}

// looksLikeEphemeralRunner reports whether runsOn matches a GitHub-hosted
// label, an Azure Microsoft-hosted pool image, or a known managed
// runner-as-a-service — the set of runners we're confident are
// provisioned fresh per job, as opposed to a persistent self-hosted
// worker (or an unrecognized custom label, which might be either).
func looksLikeEphemeralRunner(runsOn string) bool {
	lower := strings.ToLower(runsOn)
	for _, prefix := range hostedRunnerPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, prefix := range managedRunnerPrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// SEC010: self-hosted runners aren't guaranteed to run in ephemeral,
// isolated VMs the way GitHub-hosted runners do. Using one on a workflow
// that also accepts pull_request lets anyone who can open a PR execute
// code on that runner.
type selfHostedForkRunner struct{}

func (selfHostedForkRunner) Code() string             { return "SEC010" }
func (selfHostedForkRunner) Name() string             { return "self-hosted-fork-runner" }
func (selfHostedForkRunner) Severity() rules.Severity { return rules.SeverityWarning }
func (selfHostedForkRunner) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func (selfHostedForkRunner) Check(p *model.Pipeline) []rules.Violation {
	if !p.HasTrigger("pull_request") {
		return nil
	}
	var out []rules.Violation
	for _, job := range p.Jobs {
		runsOn := job.RunsOn
		if runsOn == "" || strings.Contains(runsOn, "${{") || looksLikeEphemeralRunner(runsOn) {
			continue
		}
		out = append(out, rules.Violation{
			Code:     "SEC010",
			Rule:     "self-hosted-fork-runner",
			Severity: rules.SeverityWarning,
			Message:  fmt.Sprintf("job '%s' looks like it uses a self-hosted runner ('%s') in a workflow triggered by pull_request; anyone who can open a PR can execute code on it", job.ID, runsOn),
			Path:     p.Path,
			Line:     job.Line,
			Col:      job.Col,
		})
	}
	return out
}

// SEC012: writes to GITHUB_ENV/GITHUB_PATH built from attacker-controlled
// input, inside a workflow triggered by pull_request_target/workflow_run,
// let an attacker set arbitrary environment variables (e.g. LD_PRELOAD) or
// shadow executables via PATH in later steps of the same privileged job.
type githubEnvInjection struct{}

func (githubEnvInjection) Code() string             { return "SEC012" }
func (githubEnvInjection) Name() string             { return "github-env-injection" }
func (githubEnvInjection) Severity() rules.Severity { return rules.SeverityBlocker }
func (githubEnvInjection) Platforms() []model.Platform {
	return []model.Platform{model.PlatformGitHubActions}
}

func writesGithubEnvFile(run string) bool {
	return strings.Contains(run, "GITHUB_ENV") || strings.Contains(run, "GITHUB_PATH")
}

func (githubEnvInjection) Check(p *model.Pipeline) []rules.Violation {
	if !hasDangerousTrigger(p) {
		return nil
	}
	var out []rules.Violation
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if !writesGithubEnvFile(step.Run) {
				continue
			}
			if len(findUntrustedExprs(step.Run)) == 0 {
				continue
			}
			out = append(out, rules.Violation{
				Code:     "SEC012",
				Rule:     "github-env-injection",
				Severity: rules.SeverityBlocker,
				Message:  "writes attacker-controlled input to GITHUB_ENV/GITHUB_PATH in a pull_request_target/workflow_run workflow, which can lead to code execution in later steps",
				Path:     p.Path,
				Line:     step.Line,
				Col:      step.Col,
			})
		}
	}
	return out
}
