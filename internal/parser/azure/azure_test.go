package azure

import "testing"

func TestDetect(t *testing.T) {
	cases := map[string]bool{
		"azure-pipelines.yml":         true,
		"azure-pipelines.yaml":        true,
		".azure-pipelines.yml":        true,
		"sub/dir/azure-pipelines.yml": true,
		"ci.yml":                      false,
		".github/workflows/ci.yml":    false,
	}
	for path, want := range cases {
		if got := Detect(path); got != want {
			t.Errorf("Detect(%q) = %v, want %v", path, got, want)
		}
	}
}

func TestParseStagesJobsSteps(t *testing.T) {
	p, err := Parse("azure-pipelines.yml", []byte(`
trigger:
  branches:
    include: [main]
pr:
  branches:
    include: [main]

pool:
  vmImage: ubuntu-latest

stages:
  - stage: Build
    jobs:
      - job: Compile
        timeoutInMinutes: 30
        steps:
          - checkout: self
            fetchDepth: "1"
          - task: UsePythonVersion@0
            inputs:
              versionSpec: "3.12"
          - script: pip install -r requirements.txt
          - bash: pytest
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if !p.HasTrigger("push") || !p.HasTrigger("pull_request") {
		t.Errorf("expected both push and pull_request triggers, got %v", p.OnEvents)
	}

	if len(p.Jobs) != 1 {
		t.Fatalf("expected 1 job, got %d: %+v", len(p.Jobs), p.Jobs)
	}
	job := p.Jobs[0]
	if job.ID != "Compile" || job.StageName != "Build" {
		t.Errorf("job = %+v, want ID=Compile StageName=Build", job)
	}
	if job.RunsOn != "ubuntu-latest" {
		t.Errorf("RunsOn = %q, want ubuntu-latest (inherited from root pool)", job.RunsOn)
	}
	if job.TimeoutMinutes != 30 {
		t.Errorf("TimeoutMinutes = %d, want 30", job.TimeoutMinutes)
	}
	if len(job.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d: %+v", len(job.Steps), job.Steps)
	}
	if job.Steps[0].Uses != "checkout:self" {
		t.Errorf("step[0].Uses = %q, want checkout:self", job.Steps[0].Uses)
	}
	if job.Steps[1].Uses != "UsePythonVersion@0" || job.Steps[1].With["versionSpec"] != "3.12" {
		t.Errorf("step[1] = %+v, want task UsePythonVersion@0 with versionSpec=3.12", job.Steps[1])
	}
	if job.Steps[2].Run != "pip install -r requirements.txt" {
		t.Errorf("step[2].Run = %q", job.Steps[2].Run)
	}
	if job.Steps[3].Run != "pytest" {
		t.Errorf("step[3].Run = %q", job.Steps[3].Run)
	}
}

func TestParseBareJobsNoStages(t *testing.T) {
	p, err := Parse("azure-pipelines.yml", []byte(`
jobs:
  - job: A
    pool:
      vmImage: windows-latest
    steps:
      - script: echo hi
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Jobs) != 1 || p.Jobs[0].StageName != "" {
		t.Fatalf("expected 1 job with no stage, got %+v", p.Jobs)
	}
	if p.Jobs[0].RunsOn != "windows-latest" {
		t.Errorf("RunsOn = %q, want windows-latest (job-level override)", p.Jobs[0].RunsOn)
	}
}

func TestParseImplicitJobFromBareSteps(t *testing.T) {
	p, err := Parse("azure-pipelines.yml", []byte(`
pool:
  vmImage: ubuntu-latest
steps:
  - script: echo hi
  - script: echo bye
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Jobs) != 1 {
		t.Fatalf("expected 1 implicit job, got %d", len(p.Jobs))
	}
	if len(p.Jobs[0].Steps) != 2 {
		t.Errorf("expected 2 steps in the implicit job, got %d", len(p.Jobs[0].Steps))
	}
	if p.Jobs[0].RunsOn != "ubuntu-latest" {
		t.Errorf("RunsOn = %q, want ubuntu-latest", p.Jobs[0].RunsOn)
	}
}

func TestParseNoneTriggerIsNotAnEvent(t *testing.T) {
	p, err := Parse("azure-pipelines.yml", []byte(`
trigger: none
pr: none
jobs:
  - job: A
    steps:
      - script: echo hi
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if p.HasTrigger("push") || p.HasTrigger("pull_request") {
		t.Errorf("expected no triggers from 'trigger: none'/'pr: none', got %v", p.OnEvents)
	}
}

func TestParseTemplateStepAndJobAreOpaque(t *testing.T) {
	p, err := Parse("azure-pipelines.yml", []byte(`
jobs:
  - job: A
    steps:
      - template: templates/setup.yml
        parameters:
          version: "3.12"
      - script: echo hi
  - template: templates/other-job.yml
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The templated job is skipped entirely (opaque); only "A" remains.
	if len(p.Jobs) != 1 || p.Jobs[0].ID != "A" {
		t.Fatalf("expected only job A, got %+v", p.Jobs)
	}
	if len(p.Jobs[0].Steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(p.Jobs[0].Steps))
	}
	if p.Jobs[0].Steps[0].Uses != "template:templates/setup.yml" {
		t.Errorf("step[0].Uses = %q", p.Jobs[0].Steps[0].Uses)
	}
}

// TestParseConditionalInsertionIsFlattenedNotTreatedAsAJob is a
// regression test for a bug found vetting against dotnet/roslyn's real
// azure-pipelines.yml: "${{ if ... }}:" conditional-insertion syntax
// wraps a nested job list under a single-key mapping. Before this was
// flattened, that wrapper node was itself parsed as a job — empty ID, no
// steps, no timeout — producing a misleading "job ” has no
// timeoutInMinutes" finding instead of either surfacing the real nested
// job or (as here, since it's template-included) correctly staying
// opaque.
func TestParseConditionalInsertionIsFlattenedNotTreatedAsAJob(t *testing.T) {
	p, err := Parse("azure-pipelines.yml", []byte(`
jobs:
  - job: real
    steps:
      - script: echo hi
  - ${{ if eq(variables['Build.Reason'], 'PullRequest') }}:
    - template: eng/pipelines/pr-only-job.yml
      parameters:
        configuration: Debug
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	// The wrapper contributes zero jobs of its own (its content is a
	// template reference, opaque); only the real, unconditional job
	// should show up — not a phantom job with an empty ID.
	if len(p.Jobs) != 1 || p.Jobs[0].ID != "real" {
		t.Fatalf("expected only job 'real', got %+v", p.Jobs)
	}
}

// TestParseTaskInlineScriptPopulatesRun is a regression test for a gap
// found vetting AvaloniaUI/Avalonia's azure-pipelines.yml:
// a script-running task (CmdLine@2,
// PowerShell@2, Bash@3, AzureCLI@2, ...) carries its command in
// inputs.script or inputs.inlineScript, not a top-level "script:"/
// "bash:" key. Before this was surfaced into step.Run too, every rule
// that inspects Run (PERF001, LEAN001, SEC016) silently saw nothing for
// the more common way an Azure pipeline actually runs a shell command.
func TestParseTaskInlineScriptPopulatesRun(t *testing.T) {
	p, err := Parse("azure-pipelines.yml", []byte(`
jobs:
  - job: A
    steps:
      - task: CmdLine@2
        inputs:
          script: printenv
      - task: AzureCLI@2
        inputs:
          scriptType: pscore
          scriptLocation: inlineScript
          inlineScript: az account show
      - task: UsePythonVersion@0
        inputs:
          versionSpec: "3.12"
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	steps := p.Jobs[0].Steps
	if len(steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(steps))
	}
	if steps[0].Run != "printenv" {
		t.Errorf("CmdLine@2: Run = %q, want %q (from inputs.script)", steps[0].Run, "printenv")
	}
	if steps[1].Run != "az account show" {
		t.Errorf("AzureCLI@2: Run = %q, want %q (from inputs.inlineScript)", steps[1].Run, "az account show")
	}
	// A task with no script-bearing input (a tool installer, not a
	// script runner) must not get a fabricated Run value.
	if steps[2].Run != "" {
		t.Errorf("UsePythonVersion@0: Run = %q, want empty (no script input)", steps[2].Run)
	}
}

func TestParseNestedConditionalInsertionFlattensToRealJobs(t *testing.T) {
	p, err := Parse("azure-pipelines.yml", []byte(`
jobs:
  - ${{ if eq(variables['Build.Reason'], 'PullRequest') }}:
    - ${{ each config in parameters.configs }}:
      - job: nested
        timeoutInMinutes: 20
        steps:
          - script: echo hi
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Jobs) != 1 || p.Jobs[0].ID != "nested" || p.Jobs[0].TimeoutMinutes != 20 {
		t.Fatalf("expected the doubly-nested job to flatten through, got %+v", p.Jobs)
	}
}
