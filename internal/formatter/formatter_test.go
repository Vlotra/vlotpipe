package formatter

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/vlotra/vlotpipe/internal/model"
)

func mustParse(t *testing.T, raw []byte) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal(raw, &n); err != nil {
		t.Fatalf("output is not valid YAML: %v\n---\n%s", err, raw)
	}
	return &n
}

func TestFormatIsIdempotent(t *testing.T) {
	src := []byte(`on:
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4 # a comment
      - run: echo hi
`)
	first, err := Format(model.PlatformGitHubActions, src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	second, err := Format(model.PlatformGitHubActions, first)
	if err != nil {
		t.Fatalf("Format (second pass): %v", err)
	}
	if string(first) != string(second) {
		t.Errorf("formatting an already-formatted file changed it:\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
}

func TestFormatUsesTwoSpaceIndent(t *testing.T) {
	src := []byte(`jobs:
    build:
        runs-on: ubuntu-latest
        steps:
            - run: echo hi
`)
	out, err := Format(model.PlatformGitHubActions, src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	n := mustParse(t, out)

	// The re-encoded jobs mapping's first key must sit at column 3
	// (2-space indent), regardless of the 4-space indent in the source.
	jobsVal := n.Content[0].Content[1] // doc -> "jobs" key/value pair, value is index 1
	if jobsVal.Content[0].Column != 3 {
		t.Errorf("job key column = %d, want 3 (2-space indent)", jobsVal.Content[0].Column)
	}
}

func TestFormatPreservesLineComments(t *testing.T) {
	src := []byte(`jobs:
  build:
    runs-on: ubuntu-latest # pinned intentionally
    steps:
      - run: echo hi
`)
	out, err := Format(model.PlatformGitHubActions, src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.Contains(string(out), "# pinned intentionally") {
		t.Errorf("line comment was dropped:\n%s", out)
	}
}

func TestFormatEmptyFileIsUnchanged(t *testing.T) {
	out, err := Format(model.PlatformGitHubActions, []byte(""))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if string(out) != "" {
		t.Errorf("expected an empty file to round-trip unchanged, got %q", out)
	}
}

func TestFormatRejectsInvalidYAML(t *testing.T) {
	_, err := Format(model.PlatformGitHubActions, []byte("jobs: [this is not: closed"))
	if err == nil {
		t.Fatal("expected an error for invalid YAML, got nil")
	}
}

// mappingKeys returns a mapping node's own keys, in their current order.
func mappingKeys(n *yaml.Node) []string {
	var out []string
	for i := 0; i+1 < len(n.Content); i += 2 {
		out = append(out, n.Content[i].Value)
	}
	return out
}

func TestFormatReordersGitHubWorkflowRootKeys(t *testing.T) {
	// Deliberately out of order and with an unrecognized custom key
	// ("x-internal-note") mixed in, which must survive, unranked, after
	// every recognized key.
	src := []byte(`jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
x-internal-note: for our own tooling
permissions:
  contents: read
name: CI
on:
  push:
    branches: [main]
`)
	out, err := Format(model.PlatformGitHubActions, src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	n := mustParse(t, out)
	got := mappingKeys(n.Content[0])
	want := []string{"name", "on", "permissions", "jobs", "x-internal-note"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("root key order = %v, want %v", got, want)
	}
}

func TestFormatReordersGitHubJobAndStepKeys(t *testing.T) {
	src := []byte(`jobs:
  build:
    steps:
      - with:
          fetch-depth: 1
        uses: actions/checkout@v4
        name: Checkout
    timeout-minutes: 10
    runs-on: ubuntu-latest
`)
	out, err := Format(model.PlatformGitHubActions, src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	n := mustParse(t, out)
	jobsNode := n.Content[0].Content[1] // "jobs:" value
	jobNode := jobsNode.Content[1]      // "build:" value
	jobKeys := mappingKeys(jobNode)
	wantJob := []string{"runs-on", "timeout-minutes", "steps"}
	if strings.Join(jobKeys, ",") != strings.Join(wantJob, ",") {
		t.Errorf("job key order = %v, want %v", jobKeys, wantJob)
	}

	var findSteps func(*yaml.Node) *yaml.Node
	findSteps = func(job *yaml.Node) *yaml.Node {
		for i := 0; i+1 < len(job.Content); i += 2 {
			if job.Content[i].Value == "steps" {
				return job.Content[i+1]
			}
		}
		return nil
	}
	step := findSteps(jobNode).Content[0]
	stepKeys := mappingKeys(step)
	wantStep := []string{"name", "uses", "with"}
	if strings.Join(stepKeys, ",") != strings.Join(wantStep, ",") {
		t.Errorf("step key order = %v, want %v", stepKeys, wantStep)
	}
}

func TestFormatReordersAzureRootAndJobKeys(t *testing.T) {
	src := []byte(`jobs:
  - job: Build
    timeoutInMinutes: 30
    pool:
      vmImage: ubuntu-latest
    steps:
      - script: echo hi
pool:
  vmImage: ubuntu-latest
trigger:
  - main
`)
	out, err := Format(model.PlatformAzurePipelines, src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	n := mustParse(t, out)
	rootKeys := mappingKeys(n.Content[0])
	wantRoot := []string{"trigger", "pool", "jobs"}
	if strings.Join(rootKeys, ",") != strings.Join(wantRoot, ",") {
		t.Errorf("root key order = %v, want %v", rootKeys, wantRoot)
	}

	jobsNode := n.Content[0].Content[len(n.Content[0].Content)-1] // "jobs:" is last per wantRoot
	jobNode := jobsNode.Content[0]
	jobKeys := mappingKeys(jobNode)
	wantJob := []string{"job", "pool", "timeoutInMinutes", "steps"}
	if strings.Join(jobKeys, ",") != strings.Join(wantJob, ",") {
		t.Errorf("azure job key order = %v, want %v", jobKeys, wantJob)
	}
}

func TestFormatDoesNotReorderOpaqueTemplateJobOrStage(t *testing.T) {
	// A "template:" job/stage is an opaque include (parseJobs/parseStages
	// never treat it as real) — its own keys must be left exactly as
	// written, not run through azureJobOrder/azureStageOrder.
	src := []byte(`jobs:
  - template: templates/build.yml
    parameters:
      configuration: Release
`)
	out, err := Format(model.PlatformAzurePipelines, src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	n := mustParse(t, out)
	jobsNode := n.Content[0].Content[1]
	got := mappingKeys(jobsNode.Content[0])
	want := []string{"template", "parameters"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("opaque template job keys = %v, want unchanged %v", got, want)
	}
}

func TestFormatReordersAzureTemplateStepNormally(t *testing.T) {
	// Unlike a template job/stage, a template *step* is a real,
	// recognized step kind and must be reordered like any other step.
	src := []byte(`jobs:
  - job: A
    steps:
      - parameters:
          version: "3.12"
        template: templates/setup.yml
        displayName: Setup
`)
	out, err := Format(model.PlatformAzurePipelines, src)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	n := mustParse(t, out)
	jobNode := n.Content[0].Content[1].Content[0]
	stepsNode := jobNode.Content[len(jobNode.Content)-1] // "steps" sorts last
	step := stepsNode.Content[0]
	got := mappingKeys(step)
	want := []string{"template", "displayName", "parameters"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("template step key order = %v, want %v", got, want)
	}
}

// TestFormatRealFileStaysValidAndIdempotent runs Format against every
// real-world testdata fixture in the repo — not because the formatter
// needs to preserve their exact style (it deliberately doesn't), but
// because it must never produce something that fails to parse, and
// running it twice must be a no-op the second time, the same guarantee
// internal/fixer's own idempotency test checks for --fix.
func TestFormatRealFileStaysValidAndIdempotent(t *testing.T) {
	files := []struct {
		path     string
		platform model.Platform
	}{
		{"../../testdata/github/good/.github/workflows/ci.yml", model.PlatformGitHubActions},
		{"../../testdata/github/bad/.github/workflows/ci.yml", model.PlatformGitHubActions},
		{"../../testdata/azure/good/azure-pipelines.yml", model.PlatformAzurePipelines},
		{"../../testdata/azure/bad/azure-pipelines.yml", model.PlatformAzurePipelines},
	}
	for _, f := range files {
		t.Run(f.path, func(t *testing.T) {
			raw, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			first, err := Format(f.platform, raw)
			if err != nil {
				t.Fatalf("Format: %v", err)
			}
			mustParse(t, first)
			second, err := Format(f.platform, first)
			if err != nil {
				t.Fatalf("Format (second pass): %v", err)
			}
			if string(first) != string(second) {
				t.Errorf("not idempotent on %s", f.path)
			}
		})
	}
}
