package report

import (
	"bytes"
	"strings"
	"testing"

	"github.com/vlotra/vlotpipe/internal/fingerprint"
	"github.com/vlotra/vlotpipe/internal/rules"
)

func TestGitHubAnnotationsMapsSeverityToLevel(t *testing.T) {
	violations := []rules.Violation{
		{Code: "SEC001", Severity: rules.SeverityBlocker, Message: "blocker msg", Path: "a.yml", Line: 1, Col: 2},
		{Code: "PERF001", Severity: rules.SeverityWarning, Message: "warning msg", Path: "a.yml", Line: 3, Col: 4},
		{Code: "LEAN011", Severity: rules.SeverityInfo, Message: "info msg", Path: "a.yml", Line: 5, Col: 6},
	}
	var buf bytes.Buffer
	GitHub(&buf, violations)
	out := buf.String()

	cases := []struct{ want string }{
		{"::error file=a.yml,line=1,col=2,title=SEC001::blocker msg"},
		{"::warning file=a.yml,line=3,col=4,title=PERF001::warning msg"},
		{"::notice file=a.yml,line=5,col=6,title=LEAN011::info msg"},
	}
	for _, tc := range cases {
		if !strings.Contains(out, tc.want) {
			t.Errorf("output missing %q\n--- full output ---\n%s", tc.want, out)
		}
	}
}

func TestGitHubAnnotationsEscapesMessage(t *testing.T) {
	violations := []rules.Violation{
		{Code: "X", Severity: rules.SeverityWarning, Message: "100% sure, line1\nline2\r", Path: "a.yml", Line: 1, Col: 1},
	}
	var buf bytes.Buffer
	GitHub(&buf, violations)
	out := buf.String()
	if !strings.Contains(out, "100%25 sure, line1%0Aline2%0D") {
		t.Errorf("expected %%, \\n, \\r to be escaped, got: %s", out)
	}
}

func TestAzureDevOpsMapsSeverityToIssueType(t *testing.T) {
	violations := []rules.Violation{
		{Code: "AZR002", Severity: rules.SeverityBlocker, Message: "blocker msg", Path: "azure-pipelines.yml", Line: 7, Col: 3},
		{Code: "AZR001", Severity: rules.SeverityWarning, Message: "warning msg", Path: "azure-pipelines.yml", Line: 9, Col: 1},
		{Code: "LEAN011", Severity: rules.SeverityInfo, Message: "info msg", Path: "azure-pipelines.yml", Line: 11, Col: 1},
	}
	var buf bytes.Buffer
	AzureDevOps(&buf, violations)
	out := buf.String()

	if !strings.Contains(out, "##vso[task.logissue type=error;sourcepath=azure-pipelines.yml;linenumber=7;columnnumber=3]AZR002: blocker msg") {
		t.Errorf("blocker did not map to error, output: %s", out)
	}
	if !strings.Contains(out, "##vso[task.logissue type=warning;sourcepath=azure-pipelines.yml;linenumber=9;columnnumber=1]AZR001: warning msg") {
		t.Errorf("warning did not map to warning, output: %s", out)
	}
	// Azure logging commands only define error/warning — info maps to
	// warning (the less severe of the two) rather than being dropped.
	if !strings.Contains(out, "##vso[task.logissue type=warning;sourcepath=azure-pipelines.yml;linenumber=11;columnnumber=1]LEAN011: info msg") {
		t.Errorf("info did not map to warning, output: %s", out)
	}
}

func TestGitHubAnnotationsEmptyViolationsProducesNoOutput(t *testing.T) {
	var buf bytes.Buffer
	GitHub(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for zero violations, got %q", buf.String())
	}
}

// TestDuplicateClustersPrintsLocationsAndSimilarity proves the actual
// file:line + job identity of each cluster member is printed, not just
// a count — this is free, local, single-scan output, no different from
// any other finding's precision.
func TestDuplicateClustersPrintsLocationsAndSimilarity(t *testing.T) {
	groups := []fingerprint.Group{
		{Members: []fingerprint.Chunk{
			{Path: "tests.yml", JobID: "codeception-frontend", JobName: "Codeception Frontend Tests", Line: 102, Signature: 0xF0F0F0F0F0F0F0F0},
			{Path: "tests.yml", JobID: "codeception-backend", JobName: "Codeception Backend Tests", Line: 152, Signature: 0xF0F0F0F0F0F0F0F0},
		}},
	}
	var buf bytes.Buffer
	DuplicateClusters(&buf, groups)
	out := buf.String()

	for _, want := range []string{
		"1 duplicate job cluster found",
		"100% similar",
		"tests.yml:102",
		`"Codeception Frontend Tests"`,
		"tests.yml:152",
		`"Codeception Backend Tests"`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n--- full output ---\n%s", want, out)
		}
	}
}

func TestDuplicateClustersFallsBackToJobIDWhenNameEmpty(t *testing.T) {
	groups := []fingerprint.Group{
		{Members: []fingerprint.Chunk{
			{Path: "a.yml", JobID: "build", Line: 1, Signature: 0x1},
			{Path: "b.yml", JobID: "build", Line: 1, Signature: 0x1},
		}},
	}
	var buf bytes.Buffer
	DuplicateClusters(&buf, groups)
	if !strings.Contains(buf.String(), `job "build"`) {
		t.Errorf("expected JobID fallback in output, got %q", buf.String())
	}
}

func TestDuplicateClustersEmptyProducesNoOutput(t *testing.T) {
	var buf bytes.Buffer
	DuplicateClusters(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("expected no output for zero clusters, got %q", buf.String())
	}
}
