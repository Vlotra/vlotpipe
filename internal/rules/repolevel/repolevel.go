// Package repolevel holds checks that need to look beyond a single
// workflow file — repo layout, sibling config files — rather than one
// Pipeline in isolation. These don't fit the per-pipeline rules.Rule
// interface, so the CLI calls them directly instead of through the
// registry.
package repolevel

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/rules"
)

var dependencyUpdateConfigPaths = []string{
	".github/dependabot.yml",
	".github/dependabot.yaml",
	"renovate.json",
	"renovate.json5",
	".github/renovate.json",
	".github/renovate.json5",
	".renovaterc",
	".renovaterc.json",
}

// usesThirdPartyAction reports whether the pipeline references at least
// one non-local, non-Docker action, i.e. something an update tool could
// actually track. GitHub Actions only: Dependabot/Renovate have no
// equivalent "keep this pinned" ecosystem for Azure Pipelines tasks,
// which aren't SHA-pinned in the first place (see docs/AZURE_RESEARCH.md)
// — recommending a dependabot.yml would be wrong advice there.
func usesThirdPartyAction(p *model.Pipeline) bool {
	if p.Platform != model.PlatformGitHubActions {
		return false
	}
	for _, job := range p.Jobs {
		for _, step := range job.Steps {
			if step.Uses == "" || strings.HasPrefix(step.Uses, "./") || strings.HasPrefix(step.Uses, "docker://") {
				continue
			}
			return true
		}
	}
	return false
}

// CheckDependencyUpdateTooling implements SUPPLY001: pinning actions to a
// commit SHA (SEC001) is only durable if something keeps that SHA current.
// Without Dependabot or Renovate configured for the github-actions
// ecosystem, a pinned action just silently rots instead of getting
// security patches.
func CheckDependencyUpdateTooling(repoRoot string, pipelines []*model.Pipeline) []rules.Violation {
	var firstPipelineWithAction *model.Pipeline
	for _, p := range pipelines {
		if usesThirdPartyAction(p) {
			firstPipelineWithAction = p
			break
		}
	}
	if firstPipelineWithAction == nil {
		return nil
	}

	for _, rel := range dependencyUpdateConfigPaths {
		if _, err := os.Stat(filepath.Join(repoRoot, rel)); err == nil {
			return nil
		}
	}

	return []rules.Violation{{
		Code:     "SUPPLY001",
		Rule:     "no-dependency-update-tool",
		Severity: rules.SeverityWarning,
		Message:  "no Dependabot or Renovate config found for keeping pinned actions up to date; a SHA-pinned action is only as secure as its last update",
		Path:     firstPipelineWithAction.Path,
		Line:     1,
		Col:      1,
	}}
}
