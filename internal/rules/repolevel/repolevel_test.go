package repolevel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vlotra/vlotpipe/internal/model"
)

func pipelineWithAction() *model.Pipeline {
	return &model.Pipeline{
		Platform: model.PlatformGitHubActions,
		Path:     "ci.yml",
		Jobs: []model.Job{{
			Steps: []model.Step{{Uses: "actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683"}},
		}},
	}
}

func TestFiresWithoutDependencyUpdateConfig(t *testing.T) {
	dir := t.TempDir()
	got := CheckDependencyUpdateTooling(dir, []*model.Pipeline{pipelineWithAction()})
	if len(got) != 1 || got[0].Code != "SUPPLY001" {
		t.Errorf("expected one SUPPLY001 violation, got %v", got)
	}
}

func TestDoesNotFireWithDependabotConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".github"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".github", "dependabot.yml"), []byte("version: 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CheckDependencyUpdateTooling(dir, []*model.Pipeline{pipelineWithAction()})
	if len(got) != 0 {
		t.Errorf("expected no violations with dependabot.yml present, got %v", got)
	}
}

func TestDoesNotFireWithoutThirdPartyActions(t *testing.T) {
	dir := t.TempDir()
	p := &model.Pipeline{
		Platform: model.PlatformGitHubActions,
		Path:     "ci.yml",
		Jobs:     []model.Job{{Steps: []model.Step{{Uses: "./local-action"}}}},
	}
	got := CheckDependencyUpdateTooling(dir, []*model.Pipeline{p})
	if len(got) != 0 {
		t.Errorf("expected no violations when no third-party action is used, got %v", got)
	}
}

func TestDoesNotFireForAzurePipelines(t *testing.T) {
	// Azure task versions aren't SHA-pinned the way GitHub Actions are
	// (see docs/AZURE_RESEARCH.md), and Dependabot/Renovate have no
	// "azure-pipelines" ecosystem — recommending a dependabot.yml would
	// be wrong advice here, so this must never fire for an Azure pipeline
	// regardless of how many tasks it uses.
	dir := t.TempDir()
	p := &model.Pipeline{
		Platform: model.PlatformAzurePipelines,
		Path:     "azure-pipelines.yml",
		Jobs:     []model.Job{{Steps: []model.Step{{Uses: "UsePythonVersion@0"}}}},
	}
	got := CheckDependencyUpdateTooling(dir, []*model.Pipeline{p})
	if len(got) != 0 {
		t.Errorf("expected no SUPPLY001 violations for an Azure pipeline, got %v", got)
	}
}
