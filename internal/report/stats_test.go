package report

import (
	"testing"

	"github.com/vlotra/vlotpipe/internal/rules"
)

func TestBuildStats(t *testing.T) {
	violations := []rules.Violation{
		{Code: "SEC001", Severity: rules.SeverityBlocker, Path: "a.yml"},
		{Code: "SEC001", Severity: rules.SeverityBlocker, Path: "a.yml"},
		{Code: "TIMEOUT001", Severity: rules.SeverityWarning, Path: "a.yml"},
		{Code: "TIMEOUT001", Severity: rules.SeverityWarning, Path: "b.yml"},
		{Code: "STRUCT001", Severity: rules.SeverityInfo, Path: "b.yml"},
	}

	s := BuildStats(violations, 3)

	if s.FilesScanned != 3 {
		t.Errorf("FilesScanned = %d, want 3", s.FilesScanned)
	}
	if s.TotalViolations != 5 {
		t.Errorf("TotalViolations = %d, want 5", s.TotalViolations)
	}
	if s.BySeverity["blocker"] != 2 || s.BySeverity["warning"] != 2 || s.BySeverity["info"] != 1 {
		t.Errorf("BySeverity = %+v", s.BySeverity)
	}

	if len(s.ByCode) != 3 || s.ByCode[0].Code != "SEC001" || s.ByCode[0].Count != 2 {
		t.Errorf("ByCode = %+v, want SEC001 first with count 2", s.ByCode)
	}

	if len(s.ByFile) != 2 || s.ByFile[0].Path != "a.yml" || s.ByFile[0].Blockers != 2 {
		t.Errorf("ByFile = %+v, want a.yml first (has blockers)", s.ByFile)
	}
	if s.ByFile[1].Path != "b.yml" || s.ByFile[1].Total != 2 {
		t.Errorf("ByFile[1] = %+v, want b.yml with total 2", s.ByFile[1])
	}
}

func TestBuildStatsEmpty(t *testing.T) {
	s := BuildStats(nil, 5)
	if s.TotalViolations != 0 || s.FilesScanned != 5 {
		t.Errorf("unexpected stats for empty input: %+v", s)
	}
	if len(s.ByCode) != 0 || len(s.ByFile) != 0 {
		t.Errorf("expected empty ByCode/ByFile, got %+v / %+v", s.ByCode, s.ByFile)
	}
}
