package config

import (
	"path/filepath"
	"testing"
)

func TestInitWritesFile(t *testing.T) {
	dir := t.TempDir()
	path, err := Init(dir, false)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if path != filepath.Join(dir, FileName) {
		t.Errorf("path = %q, want %q", path, filepath.Join(dir, FileName))
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Init: %v", err)
	}
	if len(cfg.Ignores) != 0 {
		t.Errorf("expected an empty ignore list from the template, got %v", cfg.Ignores)
	}
}

func TestInitRefusesToOverwriteWithoutForce(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("first Init: %v", err)
	}
	if _, err := Init(dir, false); err == nil {
		t.Error("expected second Init without --force to fail")
	}
	if _, err := Init(dir, true); err != nil {
		t.Errorf("Init with force=true should succeed, got: %v", err)
	}
}

// TestInitTemplateLeavesSelectAndReportUnset confirms the new
// select:/report: examples in the template are commented out, not
// live — the starter file must not accidentally start gating or
// narrowing anything.
func TestInitTemplateLeavesSelectAndReportUnset(t *testing.T) {
	dir := t.TempDir()
	if _, err := Init(dir, false); err != nil {
		t.Fatalf("Init: %v", err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Init: %v", err)
	}
	if len(cfg.Select) != 0 {
		t.Errorf("expected Select to be unset from the template, got %v", cfg.Select)
	}
	if len(cfg.Report.Select) != 0 {
		t.Errorf("expected Report.Select to be unset from the template, got %v", cfg.Report.Select)
	}
	if cfg.Report.To != "" {
		t.Errorf("expected Report.To to be unset from the template, got %q", cfg.Report.To)
	}
}

func TestMatchesSelectorEmptyMeansEverything(t *testing.T) {
	if !MatchesSelector("SEC001", nil) {
		t.Error("a nil selector list must match every code")
	}
	if !MatchesSelector("AZR002", []string{}) {
		t.Error("an empty selector list must match every code")
	}
}

func TestMatchesSelectorExactCode(t *testing.T) {
	if !MatchesSelector("SEC001", []string{"SEC001"}) {
		t.Error("an exact code must match itself")
	}
	if MatchesSelector("SEC002", []string{"SEC001"}) {
		t.Error("a different exact code must not match")
	}
}

func TestMatchesSelectorCategoryPrefix(t *testing.T) {
	if !MatchesSelector("SEC016", []string{"SEC"}) {
		t.Error("a category prefix must match every code in that category")
	}
	if MatchesSelector("SUPPLY001", []string{"SEC"}) {
		t.Error("\"SEC\" must not match \"SUPPLY001\" — a different category that happens to share no real prefix relationship")
	}
	if MatchesSelector("AZR001", []string{"SEC"}) {
		t.Error("an unrelated category must not match")
	}
}

func TestMatchesSelectAndReportSelectAreIndependent(t *testing.T) {
	cfg := &Config{
		Select: []string{"SEC001"},
		// Report.Select deliberately left unset — must default to
		// "everything," not fall back to mirroring Select.
	}
	if !cfg.MatchesSelect("SEC001") {
		t.Error("SEC001 should match the gate's select list")
	}
	if cfg.MatchesSelect("SEC002") {
		t.Error("SEC002 should not match the gate's select list")
	}
	if !cfg.MatchesReportSelect("SEC002") {
		t.Error("Report.Select is unset, so every code — including ones not in the narrower gate list — must still be reportable")
	}
}

func TestFixExcludedDefaultsToExcludingNothing(t *testing.T) {
	cfg := &Config{}
	if cfg.FixExcluded("TIMEOUT001") {
		t.Error("an empty Fix.Exclude must exclude nothing — the opposite default from Select/Report.Select")
	}
}

func TestFixExcludedMatchesExactCodeAndPrefix(t *testing.T) {
	cfg := &Config{Fix: FixConfig{Exclude: []string{"TIMEOUT001", "AZR"}}}
	if !cfg.FixExcluded("TIMEOUT001") {
		t.Error("TIMEOUT001 should be excluded (exact match)")
	}
	if !cfg.FixExcluded("AZR001") {
		t.Error("AZR001 should be excluded (category prefix match)")
	}
	if cfg.FixExcluded("SEC006") {
		t.Error("SEC006 should not be excluded — not on the list")
	}
}
