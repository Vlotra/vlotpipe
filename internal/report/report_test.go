package report

import (
	"bytes"
	"strings"
	"testing"

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
