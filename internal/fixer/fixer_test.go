package fixer

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/vlotra/vlotpipe/internal/model"
	azureparser "github.com/vlotra/vlotpipe/internal/parser/azure"
	ghparser "github.com/vlotra/vlotpipe/internal/parser/github"
	"github.com/vlotra/vlotpipe/internal/rules"
	_ "github.com/vlotra/vlotpipe/internal/rules/baseline"
)

// mustBeValidYAML fails the test if fixed isn't parseable — the one
// thing every fix must never do, no matter what else it gets right.
func mustBeValidYAML(t *testing.T, fixed []byte) {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal(fixed, &n); err != nil {
		t.Fatalf("fixed output is not valid YAML: %v\n---\n%s", err, fixed)
	}
}

func TestFixTimeoutMinutesGH(t *testing.T) {
	src := `jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	violations := rules.Run(p, nil)

	fixed, n, err := Fix(model.PlatformGitHubActions, []byte(src), violations, Options{})
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected at least one fix applied, got 0 (violations: %v)", violations)
	}
	mustBeValidYAML(t, fixed)

	p2, err := ghparser.Parse("inline.yml", fixed)
	if err != nil {
		t.Fatalf("re-parse after fix: %v", err)
	}
	if p2.Jobs[0].TimeoutMinutes != 30 {
		t.Errorf("TimeoutMinutes after fix = %d, want 30\n--- fixed file ---\n%s", p2.Jobs[0].TimeoutMinutes, fixed)
	}
	remaining := rules.Run(p2, nil)
	for _, v := range remaining {
		if v.Code == "TIMEOUT001" {
			t.Errorf("TIMEOUT001 still fires after fix: %v", v)
		}
	}
}

func TestFixTimeoutMinutesRespectsOptionsOverride(t *testing.T) {
	src := "jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n"
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fixed, n, err := Fix(model.PlatformGitHubActions, []byte(src), rules.Run(p, nil), Options{TimeoutMinutes: 15})
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected at least one fix applied, got 0")
	}
	p2, err := ghparser.Parse("inline.yml", fixed)
	if err != nil {
		t.Fatalf("re-parse after fix: %v", err)
	}
	if p2.Jobs[0].TimeoutMinutes != 15 {
		t.Errorf("TimeoutMinutes after fix = %d, want 15 (from Options override)\n--- fixed file ---\n%s", p2.Jobs[0].TimeoutMinutes, fixed)
	}
}

func TestFixTimeoutMinutesAzure(t *testing.T) {
	src := `jobs:
  - job: Build
    pool:
      vmImage: ubuntu-latest
    steps:
      - script: echo hi
`
	p, err := azureparser.Parse("azure-pipelines.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	violations := rules.Run(p, nil)

	fixed, n, err := Fix(model.PlatformAzurePipelines, []byte(src), violations, Options{})
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected at least one fix applied, got 0 (violations: %v)", violations)
	}
	mustBeValidYAML(t, fixed)

	p2, err := azureparser.Parse("azure-pipelines.yml", fixed)
	if err != nil {
		t.Fatalf("re-parse after fix: %v", err)
	}
	if p2.Jobs[0].TimeoutMinutes != 30 {
		t.Errorf("TimeoutMinutes after fix = %d, want 30\n--- fixed file ---\n%s", p2.Jobs[0].TimeoutMinutes, fixed)
	}
	for _, v := range rules.Run(p2, nil) {
		if v.Code == "AZR001" {
			t.Errorf("AZR001 still fires after fix: %v", v)
		}
	}
}

func TestFixPersistCredentialsCreatesWithBlock(t *testing.T) {
	src := `jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - uses: actions/checkout@` + strings.Repeat("a", 40) + `
`
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	violations := rules.Run(p, nil)

	fixed, n, err := Fix(model.PlatformGitHubActions, []byte(src), violations, Options{})
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected at least one fix applied, got 0 (violations: %v)", violations)
	}
	mustBeValidYAML(t, fixed)

	p2, err := ghparser.Parse("inline.yml", fixed)
	if err != nil {
		t.Fatalf("re-parse after fix: %v", err)
	}
	if got := p2.Jobs[0].Steps[0].With["persist-credentials"]; got != "false" {
		t.Errorf("persist-credentials after fix = %q, want %q\n--- fixed file ---\n%s", got, "false", fixed)
	}
	for _, v := range rules.Run(p2, nil) {
		if v.Code == "SEC006" {
			t.Errorf("SEC006 still fires after fix: %v", v)
		}
	}
}

func TestFixPersistCredentialsAppendsToExistingWithBlock(t *testing.T) {
	src := `jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - uses: actions/checkout@` + strings.Repeat("a", 40) + `
        with:
          fetch-depth: 1
`
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	violations := rules.Run(p, nil)

	fixed, n, err := Fix(model.PlatformGitHubActions, []byte(src), violations, Options{})
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected at least one fix applied, got 0")
	}
	mustBeValidYAML(t, fixed)

	p2, err := ghparser.Parse("inline.yml", fixed)
	if err != nil {
		t.Fatalf("re-parse after fix: %v", err)
	}
	step := p2.Jobs[0].Steps[0]
	if step.With["persist-credentials"] != "false" {
		t.Errorf("persist-credentials after fix = %q, want %q\n--- fixed file ---\n%s", step.With["persist-credentials"], "false", fixed)
	}
	// The pre-existing key must survive untouched.
	if step.With["fetch-depth"] != "1" {
		t.Errorf("fetch-depth after fix = %q, want %q (existing with: content must be preserved)", step.With["fetch-depth"], "1")
	}
}

func TestFixDoesNotTouchFlowStyleWithBlock(t *testing.T) {
	src := `jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - uses: actions/checkout@` + strings.Repeat("a", 40) + `
        with: { fetch-depth: 1 }
`
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	violations := rules.Run(p, nil)

	got := false
	for _, v := range violations {
		if v.Code == "SEC006" {
			got = true
		}
	}
	if !got {
		t.Fatalf("expected SEC006 to fire on the fixture, it didn't")
	}

	fixed, n, err := Fix(model.PlatformGitHubActions, []byte(src), violations, Options{})
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if n != 0 {
		t.Errorf("expected the flow-style with: block to be left alone (n=0), got n=%d\n--- fixed file ---\n%s", n, fixed)
	}
	if string(fixed) != src {
		t.Errorf("expected an untouched file when the fix bails out, got a diff:\n%s", fixed)
	}
}

func TestFixRetentionDays(t *testing.T) {
	src := `jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - uses: actions/upload-artifact@` + strings.Repeat("a", 40) + `
        with:
          name: logs
          path: logs/
`
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	violations := rules.Run(p, nil)

	fixed, n, err := Fix(model.PlatformGitHubActions, []byte(src), violations, Options{})
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected at least one fix applied, got 0")
	}
	mustBeValidYAML(t, fixed)

	p2, err := ghparser.Parse("inline.yml", fixed)
	if err != nil {
		t.Fatalf("re-parse after fix: %v", err)
	}
	step := p2.Jobs[0].Steps[0]
	if step.With["retention-days"] != "7" {
		t.Errorf("retention-days after fix = %q, want %q", step.With["retention-days"], "7")
	}
	if step.With["name"] != "logs" || step.With["path"] != "logs/" {
		t.Errorf("existing with: content not preserved: %v", step.With)
	}
}

func TestFixConcurrency(t *testing.T) {
	src := `on:
  pull_request:
    branches: [main]

jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - run: echo hi
`
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	violations := rules.Run(p, nil)

	fixed, n, err := Fix(model.PlatformGitHubActions, []byte(src), violations, Options{})
	if err != nil {
		t.Fatalf("Fix: %v", err)
	}
	if n == 0 {
		t.Fatalf("expected at least one fix applied, got 0 (violations: %v)", violations)
	}
	mustBeValidYAML(t, fixed)

	p2, err := ghparser.Parse("inline.yml", fixed)
	if err != nil {
		t.Fatalf("re-parse after fix: %v", err)
	}
	if !p2.Concurrency {
		t.Errorf("Concurrency after fix = false, want true\n--- fixed file ---\n%s", fixed)
	}
	for _, v := range rules.Run(p2, nil) {
		if v.Code == "PERF002" {
			t.Errorf("PERF002 still fires after fix: %v", v)
		}
	}
}

// TestFixIsIdempotent runs the fixer twice on the same file. The second
// pass must find nothing left to do — a fix that isn't fully applied
// the first time (or that somehow reintroduces the condition it fixed)
// would loop forever under repeated "vlotpipe check --fix" runs.
func TestFixIsIdempotent(t *testing.T) {
	src := `jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@` + strings.Repeat("a", 40) + `
      - uses: actions/upload-artifact@` + strings.Repeat("b", 40) + `
        with:
          name: logs
          path: logs/
`
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	fixed1, n1, err := Fix(model.PlatformGitHubActions, []byte(src), rules.Run(p, nil), Options{})
	if err != nil {
		t.Fatalf("first Fix: %v", err)
	}
	if n1 == 0 {
		t.Fatalf("expected fixes on the first pass, got 0")
	}
	mustBeValidYAML(t, fixed1)

	p2, err := ghparser.Parse("inline.yml", fixed1)
	if err != nil {
		t.Fatalf("re-parse after first fix: %v", err)
	}
	remaining := rules.Run(p2, nil)
	fixed2, n2, err := Fix(model.PlatformGitHubActions, fixed1, remaining, Options{})
	if err != nil {
		t.Fatalf("second Fix: %v", err)
	}
	if n2 != 0 {
		t.Errorf("second pass applied %d more fixes, want 0 (nothing left to fix): %v", n2, remaining)
	}
	if string(fixed2) != string(fixed1) {
		t.Errorf("second pass changed an already-fixed file")
	}
}
