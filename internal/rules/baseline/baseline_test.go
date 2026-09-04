package baseline

import (
	"testing"

	azureparser "github.com/vlotra/vlotpipe/internal/parser/azure"
	ghparser "github.com/vlotra/vlotpipe/internal/parser/github"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func codes(vs []rules.Violation) map[string]int {
	out := map[string]int{}
	for _, v := range vs {
		out[v.Code]++
	}
	return out
}

func severityOf(vs []rules.Violation, code string) (rules.Severity, bool) {
	for _, v := range vs {
		if v.Code == code {
			return v.Severity, true
		}
	}
	return "", false
}

func TestSelfHostedRunnerDowngradesCacheConfidence(t *testing.T) {
	yamlFor := func(runsOn string) string {
		return `
jobs:
  build:
    runs-on: ` + runsOn + `
    steps:
      - run: npm ci
      - uses: docker/build-push-action@ca877d9245402d1537745e0e356eab639262a132
        with:
          push: true
`
	}

	cases := []struct {
		name         string
		runsOn       string
		wantSeverity rules.Severity
	}{
		{"GitHub-hosted runner: full confidence", "ubuntu-latest", rules.SeverityWarning},
		{"known managed-ephemeral runner: full confidence", "depot-ubuntu-24.04-4", rules.SeverityWarning},
		{"unrecognized/self-hosted-looking runner: downgraded", "my-office-mac-mini", rules.SeverityInfo},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ghparser.Parse("inline.yml", []byte(yamlFor(tc.runsOn)))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			violations := rules.Run(p, nil)

			for _, code := range []string{"PERF001", "LEAN010"} {
				got, found := severityOf(violations, code)
				if !found {
					t.Errorf("%s: expected %s to fire, it didn't (violations: %v)", tc.name, code, violations)
					continue
				}
				if got != tc.wantSeverity {
					t.Errorf("%s: %s severity = %q, want %q", tc.name, code, got, tc.wantSeverity)
				}
			}
		})
	}
}

// TestCachedRunnersSuppressPerf001AndLean010 covers the rules.<CODE>.cached_runners
// special property (ADR 0002): a runner label the repo owner asserts
// already has persistent caching suppresses PERF001/LEAN010 entirely on
// a matching job — not just the severity downgrade
// TestSelfHostedRunnerDowngradesCacheConfidence exercises above.
func TestCachedRunnersSuppressPerf001AndLean010(t *testing.T) {
	t.Cleanup(func() {
		SetCachedRunners("PERF001", nil)
		SetCachedRunners("LEAN010", nil)
	})

	src := `
jobs:
  build:
    runs-on: my-office-mac-mini
    steps:
      - run: npm ci
      - uses: docker/build-push-action@ca877d9245402d1537745e0e356eab639262a132
        with:
          push: true
`
	p, err := ghparser.Parse("inline.yml", []byte(src))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if got := codes(rules.Run(p, nil)); got["PERF001"] == 0 || got["LEAN010"] == 0 {
		t.Fatalf("expected both PERF001 and LEAN010 to fire before SetCachedRunners, got %v", got)
	}

	SetCachedRunners("PERF001", []string{"my-office-mac-mini"})
	SetCachedRunners("LEAN010", []string{"*"})

	got := codes(rules.Run(p, nil))
	if got["PERF001"] != 0 {
		t.Errorf("PERF001 should be suppressed for a runner on its cached_runners list, got %d hits", got["PERF001"])
	}
	if got["LEAN010"] != 0 {
		t.Errorf("LEAN010 should be suppressed by the \"*\" wildcard, got %d hits", got["LEAN010"])
	}
}

func TestInlineSuppressionComment(t *testing.T) {
	p, err := ghparser.Parse("inline.yml", []byte(`
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    secrets: inherit # vlotpipe: ignore[SEC007]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := codes(rules.Run(p, nil))
	if got["SEC007"] != 0 {
		t.Errorf("expected SEC007 to be suppressed by the inline comment, got %d hits", got["SEC007"])
	}
}

func TestBadWorkflowTriggersExpectedRules(t *testing.T) {
	p, err := ghparser.ParseFile("../../../testdata/github/bad/.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := codes(rules.Run(p, nil))

	want := map[string]int{
		"SEC001":     2,
		"SEC002":     1,
		"SEC005":     1,
		"SEC006":     1,
		"PERF001":    1,
		"TIMEOUT001": 1,
		"STRUCT001":  1,
	}
	for code, n := range want {
		if got[code] != n {
			t.Errorf("code %s: got %d violations, want %d", code, got[code], n)
		}
	}
}

func TestGoodWorkflowIsClean(t *testing.T) {
	p, err := ghparser.ParseFile("../../../testdata/github/good/.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := rules.Run(p, nil)
	if len(got) != 0 {
		t.Errorf("expected no violations on the good fixture, got %v", got)
	}
}

func TestBadAzurePipelineTriggersExpectedRules(t *testing.T) {
	p, err := azureparser.ParseFile("../../../testdata/azure/bad/azure-pipelines.yml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := codes(rules.Run(p, nil))

	want := map[string]int{
		"AZR001":    1,
		"AZR002":    1,
		"PERF001":   1,
		"STRUCT001": 1,
	}
	for code, n := range want {
		if got[code] != n {
			t.Errorf("code %s: got %d violations, want %d", code, got[code], n)
		}
	}
	// GitHub-only rules must never fire for an Azure pipeline.
	for _, ghOnly := range []string{"SEC001", "SEC002", "SEC005", "SEC006", "TIMEOUT001", "SUPPLY001"} {
		if got[ghOnly] != 0 {
			t.Errorf("GitHub-only code %s fired for an Azure pipeline: %d hits", ghOnly, got[ghOnly])
		}
	}
}

func TestGoodAzurePipelineIsClean(t *testing.T) {
	p, err := azureparser.ParseFile("../../../testdata/azure/good/azure-pipelines.yml")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := rules.Run(p, nil)
	if len(got) != 0 {
		t.Errorf("expected no violations on the good Azure fixture, got %v", got)
	}
}

func TestUnpinnedActionIgnoresLocalAndDockerUses(t *testing.T) {
	p, err := ghparser.Parse("inline.yml", []byte(`
jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - uses: ./local-action
      - uses: docker://alpine:3.19
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := codes(rules.Run(p, nil))
	if got["SEC001"] != 0 {
		t.Errorf("expected local/docker uses to be ignored, got %d SEC001 violations", got["SEC001"])
	}
}

func TestNewRules(t *testing.T) {
	cases := []struct {
		name string
		code string
		want bool
		yaml string
	}{
		{
			name: "SEC003 fires on pull_request_target checking out PR head",
			code: "SEC003", want: true,
			yaml: `
on: pull_request_target
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
        with:
          ref: ${{ github.event.pull_request.head.sha }}
`,
		},
		{
			name: "SEC003 fires on issue_comment ChatOps checkout by PR number",
			code: "SEC003", want: true,
			yaml: `
on: issue_comment
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
        with:
          ref: refs/pull/${{ github.event.issue.number }}/head
`,
		},
		{
			name: "SEC003 does not fire on issue_comment with no checkout",
			code: "SEC003", want: false,
			yaml: `
on: issue_comment
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "just reacting to a comment"
`,
		},
		{
			name: "SEC003 does not fire without an explicit untrusted ref",
			code: "SEC003", want: false,
			yaml: `
on: pull_request_target
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
`,
		},
		{
			name: "SEC004 fires on untrusted context in run:",
			code: "SEC004", want: true,
			yaml: `
on: pull_request
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ github.event.issue.title }}"
`,
		},
		{
			name: "SEC004 does not fire on trusted context",
			code: "SEC004", want: false,
			yaml: `
on: pull_request
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ github.event.pull_request.number }}"
`,
		},
		{
			name: "SEC005 fires when no permissions are set anywhere",
			code: "SEC005", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC005 does not fire when workflow-level permissions are set",
			code: "SEC005", want: false,
			yaml: `
permissions: {}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC006 does not fire on an explicit, deliberate persist-credentials: true",
			code: "SEC006", want: false,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
        with:
          persist-credentials: true
      - run: git push
`,
		},
		{
			name: "SEC010 does not fire on a third-party managed runner service",
			code: "SEC010", want: false,
			yaml: `
on: pull_request
jobs:
  build:
    runs-on: depot-ubuntu-24.04-4
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC007 fires on secrets: inherit",
			code: "SEC007", want: true,
			yaml: `
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    secrets: inherit
`,
		},
		{
			name: "SEC007 does not fire on explicit secret forwarding",
			code: "SEC007", want: false,
			yaml: `
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    secrets:
      token: ${{ secrets.token }}
`,
		},
		{
			name: "SEC008 fires on toJSON(secrets)",
			code: "SEC008", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: ./deploy.sh
        env:
          SECRETS: ${{ toJSON(secrets) }}
`,
		},
		{
			name: "SEC010 fires on a custom runner label with pull_request trigger",
			code: "SEC010", want: true,
			yaml: `
on: pull_request
jobs:
  build:
    runs-on: my-custom-runner
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC010 does not fire on a GitHub-hosted runner",
			code: "SEC010", want: false,
			yaml: `
on: pull_request
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC011 fires on spoofable github.actor check",
			code: "SEC011", want: true,
			yaml: `
jobs:
  automerge:
    runs-on: ubuntu-latest
    if: github.actor == 'dependabot[bot]'
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC011 does not fire on pull_request.user.login check",
			code: "SEC011", want: false,
			yaml: `
jobs:
  automerge:
    runs-on: ubuntu-latest
    if: github.event.pull_request.user.login == 'dependabot[bot]'
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC012 fires on GITHUB_ENV write with untrusted input on a dangerous trigger",
			code: "SEC012", want: true,
			yaml: `
on: pull_request_target
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ github.event.issue.title }}" >> $GITHUB_ENV
`,
		},
		{
			name: "SEC012 does not fire outside a dangerous trigger",
			code: "SEC012", want: false,
			yaml: `
on: pull_request
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ github.event.issue.title }}" >> $GITHUB_ENV
`,
		},
		{
			name: "SEC013 fires on ACTIONS_ALLOW_UNSECURE_COMMANDS",
			code: "SEC013", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "::add-path::$HOME/bin"
        env:
          ACTIONS_ALLOW_UNSECURE_COMMANDS: true
`,
		},
		{
			name: "SEC014 fires on hardcoded container password",
			code: "SEC014", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    container:
      image: fake.example.com/example
      credentials:
        username: user
        password: hackme
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC014 does not fire when password references a secret",
			code: "SEC014", want: false,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    container:
      image: fake.example.com/example
      credentials:
        username: user
        password: ${{ secrets.REGISTRY_PASSWORD }}
    steps:
      - run: echo hi
`,
		},
		{
			name: "SEC015 fires on caching in a release-triggered workflow",
			code: "SEC015", want: true,
			yaml: `
on: release
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/cache@0c907a75c2c80ebcb7f088228285e798b750cf8f
        with:
          path: node_modules
          key: cache-key
`,
		},
		{
			name: "SEC015 does not fire outside a release trigger",
			code: "SEC015", want: false,
			yaml: `
on: push
jobs:
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/cache@0c907a75c2c80ebcb7f088228285e798b750cf8f
        with:
          path: node_modules
          key: cache-key
`,
		},
		{
			name: "PERF002 fires on pull_request trigger without a concurrency group",
			code: "PERF002", want: true,
			yaml: `
on: pull_request
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`,
		},
		{
			name: "PERF002 does not fire when a concurrency group is set",
			code: "PERF002", want: false,
			yaml: `
on: pull_request
concurrency:
  group: ${{ github.ref }}
  cancel-in-progress: true
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`,
		},
		{
			name: "PERF003 fires on a superfluous action",
			code: "PERF003", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: dtolnay/rust-toolchain@086dfa4efe372cfb6b375460a56e26a62a873d2e
`,
		},
		{
			name: "LEAN001 fires on apt-get installing a language runtime",
			code: "LEAN001", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: sudo apt-get update && sudo apt-get install -y python3.12 python3-pip
`,
		},
		{
			name: "LEAN001 does not fire on unrelated apt-get installs",
			code: "LEAN001", want: false,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: sudo apt-get update && sudo apt-get install -y curl jq
`,
		},
		{
			name: "LEAN002 fires on fetch-depth: 0 with no history-walking step",
			code: "LEAN002", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
        with:
          fetch-depth: "0"
      - run: npm test
`,
		},
		{
			name: "LEAN002 does not fire when a later step needs history",
			code: "LEAN002", want: false,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
        with:
          fetch-depth: "0"
      - run: git describe --tags
`,
		},
		{
			name: "LEAN010 fires on docker/build-push-action without cache-from/cache-to",
			code: "LEAN010", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: docker/build-push-action@ca877d9245402d1537745e0e356eab639262a132
        with:
          push: true
`,
		},
		{
			name: "LEAN010 does not fire when cache-from and cache-to are set",
			code: "LEAN010", want: false,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: docker/build-push-action@ca877d9245402d1537745e0e356eab639262a132
        with:
          push: true
          cache-from: type=gha
          cache-to: type=gha,mode=max
`,
		},
		{
			name: "LEAN011 fires on upload-artifact without retention-days",
			code: "LEAN011", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
        with:
          name: logs
          path: test-results/
`,
		},
		{
			name: "LEAN011 does not fire when retention-days is set",
			code: "LEAN011", want: false,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/upload-artifact@ea165f8d65b6e75b540449e92b4886f43607fa02
        with:
          name: logs
          path: test-results/
          retention-days: 3
`,
		},
		{
			name: "LEAN008 fires when two jobs share the same first 3 steps",
			code: "LEAN008", want: true,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
      - uses: actions/setup-node@60edb5dd545a775178f52524783378180af0d1e
        with:
          node-version: "20"
      - run: npm ci
      - run: npm test
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
      - uses: actions/setup-node@60edb5dd545a775178f52524783378180af0d1e
        with:
          node-version: "20"
      - run: npm ci
      - run: npm publish
`,
		},
		{
			name: "LEAN008 does not fire when the shared prefix differs",
			code: "LEAN008", want: false,
			yaml: `
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
      - uses: actions/setup-node@60edb5dd545a775178f52524783378180af0d1e
        with:
          node-version: "20"
      - run: npm ci
  publish:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
      - uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e
        with:
          go-version: "1.23"
      - run: go build ./...
`,
		},
		{
			name: "LEAN008 does not fire on jobs with fewer than 3 steps",
			code: "LEAN008", want: false,
			yaml: `
jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
  b:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ghparser.Parse("inline.yml", []byte(tc.yaml))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := codes(rules.Run(p, nil))[tc.code] > 0
			if got != tc.want {
				t.Errorf("%s: got fired=%v, want fired=%v (violations: %v)", tc.code, got, tc.want, rules.Run(p, nil))
			}
		})
	}
}

func TestEnvironmentDump(t *testing.T) {
	cases := []struct {
		name string
		want bool
		gh   string // GitHub Actions "run:" block content, if set
	}{
		{"bare printenv fires", true, "printenv"},
		{"bare env fires", true, "env"},
		{"printenv piped to grep still fires", true, "printenv | grep TOKEN"},
		{"chained after && still fires", true, "dotnet --info && printenv"},
		{"printenv with a variable name does not fire", false, "printenv HOME"},
		{"env used to set a var for one command does not fire", false, "env FOO=bar dotnet build"},
		{"set -e does not fire", false, "set -e"},
		{"unrelated command does not fire", false, "dotnet --info"},
		{"envsubst is not env", false, "envsubst < a.tmpl > a.out"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := ghparser.Parse("inline.yml", []byte(`
jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 5
    steps:
      - run: `+tc.gh+`
`))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			got := codes(rules.Run(p, nil))["SEC016"] > 0
			if got != tc.want {
				t.Errorf("got fired=%v, want fired=%v", got, tc.want)
			}
		})
	}
}

// Regression test for the gap found vetting AvaloniaUI/Avalonia
// (docs/VETTING_AVALONIA.md): Azure's task-based steps (task:
// CmdLine@2, PowerShell@2, Bash@3, AzureCLI@2) carry their inline
// script in inputs.script/inlineScript, not step.Run — the first cut
// of this rule only checked step.Run and silently missed both of
// Avalonia's real "printenv" debug steps, both written as task:
// CmdLine@2 rather than the shorthand "script:" step.
func TestEnvironmentDumpFiresOnAzureTaskInlineScript(t *testing.T) {
	p, err := azureparser.Parse("azure-pipelines.yml", []byte(`
jobs:
  - job: Linux
    pool:
      vmImage: ubuntu-24.04
    steps:
      - task: CmdLine@2
        displayName: 'Run Build'
        inputs:
          script: |
            dotnet --info
            printenv
            ./build.sh
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := codes(rules.Run(p, nil))
	if got["SEC016"] != 1 {
		t.Fatalf("expected SEC016 to fire once for the task's inline printenv, got %d (violations: %v)", got["SEC016"], rules.Run(p, nil))
	}
}

func jobWithNSteps(id string, n int) string {
	yaml := "  " + id + ":\n    runs-on: ubuntu-latest\n    steps:\n"
	for i := 0; i < n; i++ {
		yaml += "      - run: echo step\n"
	}
	return yaml
}

func TestCheckMaxStepsPerJob(t *testing.T) {
	yaml := "jobs:\n" + jobWithNSteps("small", 5) + jobWithNSteps("big", 25)
	p, err := ghparser.Parse("inline.yml", []byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	// Default threshold (0 -> DefaultMaxStepsPerJob): only "big" (25 steps) fires.
	got := CheckMaxStepsPerJob(p, 0)
	if len(got) != 1 || got[0].Path != "inline.yml" {
		t.Fatalf("default threshold: expected exactly 1 violation, got %v", got)
	}

	// Explicit, looser threshold: nothing fires.
	if got := CheckMaxStepsPerJob(p, 30); len(got) != 0 {
		t.Errorf("threshold=30: expected no violations, got %v", got)
	}

	// Explicit, tighter threshold: both jobs fire.
	if got := CheckMaxStepsPerJob(p, 3); len(got) != 2 {
		t.Errorf("threshold=3: expected 2 violations, got %v", got)
	}
}
