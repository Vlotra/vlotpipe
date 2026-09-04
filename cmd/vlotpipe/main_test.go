package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything written to it — needed because report.Text/JSON/etc. all
// write straight to os.Stdout rather than an injectable io.Writer.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w
	fn()
	w.Close()
	os.Stdout = orig
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading captured stdout: %v", err)
	}
	return string(out)
}

// writeMixedSeverityFixture writes a workflow with two distinct
// blocker-severity findings (SEC001: unpinned action, SEC002: hardcoded
// credential) plus a warning (TIMEOUT001) and an info (STRUCT001) —
// enough spread across codes and severities to prove select/
// report-select narrow independently of each other and of --severity.
func writeMixedSeverityFixture(t *testing.T, repo string) {
	t.Helper()
	wfDir := filepath.Join(repo, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	wf := "on: push\n" +
		"permissions:\n" +
		"  contents: read\n" +
		"jobs:\n" +
		"  build:\n" +
		"    runs-on: ubuntu-latest\n" +
		"    steps:\n" +
		"      - uses: actions/checkout@v4\n" +
		"        with:\n" +
		"          api_key: hardcoded-not-a-secrets-reference\n"
	if err := os.WriteFile(filepath.Join(wfDir, "ci.yml"), []byte(wf), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func resetFlags() (restore func()) {
	orig := struct {
		severity, cfg, format, selectFlag, reportSelect, reportTo string
		fix, stats                                                bool
	}{flagSeverity, flagConfig, flagFormat, flagSelect, flagReportSelect, flagReportTo, flagFix, flagStats}
	flagSeverity, flagConfig, flagFormat = "info", "", "text"
	flagSelect, flagReportSelect, flagReportTo = "", "", ""
	flagFix, flagStats = false, false
	return func() {
		flagSeverity, flagConfig, flagFormat = orig.severity, orig.cfg, orig.format
		flagSelect, flagReportSelect, flagReportTo = orig.selectFlag, orig.reportSelect, orig.reportTo
		flagFix, flagStats = orig.fix, orig.stats
	}
}

func TestSyntaxViolationExtractsLineNumber(t *testing.T) {
	cases := []struct {
		name     string
		errText  string
		wantLine int
	}{
		{
			name:     "tab character error, wrapped with path prefix",
			errText:  "ci.yml: yaml: line 7: found a tab character that violates indentation",
			wantLine: 8, // yaml.v3 reports 0-indexed; +1 to the physical line
		},
		{
			name:     "unclosed flow mapping error, wrapped with path prefix",
			errText:  "ci.yml: yaml: line 6: did not find expected ',' or '}'",
			wantLine: 7,
		},
		{
			name:     "no line number in the message at all",
			errText:  "ci.yml: some unrelated failure",
			wantLine: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := syntaxViolation("ci.yml", errors.New(tc.errText))
			if v.Line != tc.wantLine {
				t.Errorf("Line = %d, want %d", v.Line, tc.wantLine)
			}
			if v.Code != "YAML001" {
				t.Errorf("Code = %q, want YAML001", v.Code)
			}
		})
	}
}

func TestSyntaxViolationMessageDoesNotRepeatThePath(t *testing.T) {
	v := syntaxViolation("some/deep/path/ci.yml", errors.New("some/deep/path/ci.yml: yaml: line 3: bad indentation"))
	if got := v.Message; got != "invalid YAML: yaml: line 3: bad indentation" {
		t.Errorf("Message = %q, want the path prefix stripped", got)
	}
}

// TestRunFailsCheckOnBrokenYAML is a regression test for a real bug: a
// pipeline file with invalid YAML (a tab character, an unclosed flow
// mapping, ...) used to print an error to stderr and then get silently
// skipped — "vlotpipe check" still reported "All checks passed!" and
// returned 0 blockers, exactly the case a CI gate most needs to catch.
func TestRunFailsCheckOnBrokenYAML(t *testing.T) {
	dir := t.TempDir()
	wfDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	broken := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n        with: {foo: unclosed\n"
	if err := os.WriteFile(filepath.Join(wfDir, "ci.yml"), []byte(broken), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	origSeverity, origConfig, origFix, origFormat, origStats := flagSeverity, flagConfig, flagFix, flagFormat, flagStats
	flagSeverity, flagConfig, flagFix, flagFormat, flagStats = "info", "", false, "text", false
	t.Cleanup(func() {
		flagSeverity, flagConfig, flagFix, flagFormat, flagStats = origSeverity, origConfig, origFix, origFormat, origStats
	})

	blockers, err := run([]string{dir}, true, false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if blockers == 0 {
		t.Fatal("expected at least one blocker for a file with invalid YAML, got 0 — \"check\" would wrongly exit 0")
	}
}

func TestResolveConfigDirWalksUpFromANestedFile(t *testing.T) {
	repo := t.TempDir()
	wfDir := filepath.Join(repo, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".vlotpipe.yml"), []byte("ignore: []\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ciFile := filepath.Join(wfDir, "ci.yml")
	if err := os.WriteFile(ciFile, []byte("on: push\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := resolveConfigDir(ciFile)
	if got != repo {
		t.Errorf("resolveConfigDir(%q) = %q, want %q", ciFile, got, repo)
	}
}

func TestResolveConfigDirStopsAtGitRootWhenNoConfigExists(t *testing.T) {
	repo := t.TempDir()
	wfDir := filepath.Join(repo, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	ciFile := filepath.Join(wfDir, "ci.yml")
	if err := os.WriteFile(ciFile, []byte("on: push\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got := resolveConfigDir(ciFile)
	if got != repo {
		t.Errorf("resolveConfigDir(%q) = %q, want the .git-marked repo root %q", ciFile, got, repo)
	}
}

// TestRunRespectsConfigWhenScanningAnExplicitFilePath is a regression
// test for a real bug: --config's default ("first scanned path") used
// to be passed straight to config.Load as-is, which only ever worked
// when that path was already a directory. A pre-commit hook or a CI
// step invokes vlotpipe with individual staged file paths — exactly
// what this test does — and config.Load would hard error trying to
// stat "<file path>/.vlotpipe.yml", "not a directory", meaning
// .vlotpipe.yml's exceptions silently never applied under the single
// most common real-world invocation pattern outside "scan the whole
// repo interactively."
func TestRunRespectsConfigWhenScanningAnExplicitFilePath(t *testing.T) {
	repo := t.TempDir()
	wfDir := filepath.Join(repo, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	cfgYAML := "ignore:\n  - code: SUPPLY001\n    path: \"*\"\n    reason: \"test\"\n"
	if err := os.WriteFile(filepath.Join(repo, ".vlotpipe.yml"), []byte(cfgYAML), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	ciFile := filepath.Join(wfDir, "ci.yml")
	wf := "on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    permissions:\n      contents: read\n    timeout-minutes: 5\n    steps:\n      - run: echo hi\n"
	if err := os.WriteFile(ciFile, []byte(wf), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	origSeverity, origConfig, origFix, origFormat, origStats := flagSeverity, flagConfig, flagFix, flagFormat, flagStats
	flagSeverity, flagConfig, flagFix, flagFormat, flagStats = "info", "", false, "text", false
	t.Cleanup(func() {
		flagSeverity, flagConfig, flagFix, flagFormat, flagStats = origSeverity, origConfig, origFix, origFormat, origStats
	})

	// Scanning the *file*, not its directory — the pre-commit/CI
	// pattern. Before the fix, this returned a hard error ("not a
	// directory") instead of loading .vlotpipe.yml at all.
	if _, err := run([]string{ciFile}, false, false); err != nil {
		t.Fatalf("run: %v", err)
	}
}

// TestSelectNarrowsBlockersIndependentlyOfReportSelect proves --select
// controls check's exit code (via the returned blocker count) without
// needing --report-select to also be set — the two must be independent,
// not one implicitly narrowing the other.
func TestSelectNarrowsBlockersIndependentlyOfReportSelect(t *testing.T) {
	repo := t.TempDir()
	writeMixedSeverityFixture(t, repo)
	restore := resetFlags()
	defer restore()

	// A --select that matches neither real blocker code present
	// (SEC001, SEC002): the build must not fail, even though real
	// blockers exist in the fixture.
	flagSelect = "SEC999"
	_ = captureStdout(t, func() {
		blockers, err := run([]string{repo}, true, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if blockers != 0 {
			t.Errorf("blockers = %d, want 0 — select=SEC999 matches neither SEC001 nor SEC002", blockers)
		}
	})

	// A --select that does match one of the real blocker codes: the
	// build must fail.
	flagSelect = "SEC001"
	_ = captureStdout(t, func() {
		blockers, err := run([]string{repo}, true, false)
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		if blockers == 0 {
			t.Error("blockers = 0, want > 0 — select=SEC001 should match the real SEC001 finding")
		}
	})
}

// TestReportSelectNarrowsOutputIndependentlyOfSelect proves
// --report-select controls what's printed without affecting the gate —
// leaving --select unset (matching everything) while --report-select
// narrows to one code must still show only that code, and the fixture's
// other real findings (SEC002, TIMEOUT001, STRUCT001, ...) must not
// appear.
func TestReportSelectNarrowsOutputIndependentlyOfSelect(t *testing.T) {
	repo := t.TempDir()
	writeMixedSeverityFixture(t, repo)
	restore := resetFlags()
	defer restore()

	flagReportSelect = "SEC001"
	out := captureStdout(t, func() {
		if _, err := run([]string{repo}, false, false); err != nil {
			t.Fatalf("run: %v", err)
		}
	})

	if !strings.Contains(out, "SEC001") {
		t.Errorf("expected SEC001 in output with report-select=SEC001, got:\n%s", out)
	}
	for _, unwanted := range []string{"SEC002", "TIMEOUT001", "STRUCT001"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("expected %s to be filtered out by report-select=SEC001, got it in output:\n%s", unwanted, out)
		}
	}
}

func TestInitGitHubWorkflowWritesFile(t *testing.T) {
	dir := t.TempDir()
	path, wrote, err := initGitHubWorkflow(dir, false)
	if err != nil {
		t.Fatalf("initGitHubWorkflow: %v", err)
	}
	if !wrote {
		t.Fatal("expected wrote=true for a fresh directory")
	}
	wantPath := filepath.Join(dir, ".github", "workflows", "vlotpipe.yml")
	if path != wantPath {
		t.Errorf("path = %q, want %q", path, wantPath)
	}
	if _, err := os.Stat(wantPath); err != nil {
		t.Errorf("expected %s to exist: %v", wantPath, err)
	}
}

func TestInitGitHubWorkflowSkipsExistingWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := initGitHubWorkflow(dir, false); err != nil {
		t.Fatalf("first call: %v", err)
	}
	wfPath := filepath.Join(dir, ".github", "workflows", "vlotpipe.yml")
	if err := os.WriteFile(wfPath, []byte("custom content\n"), 0o644); err != nil {
		t.Fatalf("overwriting with sentinel content: %v", err)
	}

	_, wrote, err := initGitHubWorkflow(dir, false)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if wrote {
		t.Error("expected wrote=false when the file already exists and force=false")
	}
	got, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "custom content\n" {
		t.Error("expected the existing file to be left untouched without --force")
	}
}

func TestInitGitHubWorkflowOverwritesWithForce(t *testing.T) {
	dir := t.TempDir()
	wfDir := filepath.Join(dir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	wfPath := filepath.Join(wfDir, "vlotpipe.yml")
	if err := os.WriteFile(wfPath, []byte("stale content\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	_, wrote, err := initGitHubWorkflow(dir, true)
	if err != nil {
		t.Fatalf("initGitHubWorkflow: %v", err)
	}
	if !wrote {
		t.Error("expected wrote=true with force=true")
	}
	got, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if strings.Contains(string(got), "stale content") {
		t.Error("expected the stale content to be overwritten")
	}
}

func TestSuggestAzureStepOnlyWhenAzurePipelineExists(t *testing.T) {
	dir := t.TempDir()
	if got := suggestAzureStep(dir); got != "" {
		t.Errorf("expected no suggestion when azure-pipelines.yml doesn't exist, got %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "azure-pipelines.yml"), []byte("trigger: none\n"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got := suggestAzureStep(dir)
	if !strings.Contains(got, "vlotpipe check .") {
		t.Errorf("expected the suggested snippet to run vlotpipe check ., got:\n%s", got)
	}
}
