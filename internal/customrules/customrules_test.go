package customrules

import (
	"testing"

	"github.com/vlotra/vlotpipe/internal/config"
	ghparser "github.com/vlotra/vlotpipe/internal/parser/github"
)

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

func TestTimeoutEqualsDefaultCatchesTheLoophole(t *testing.T) {
	ruleset, err := Compile([]config.CustomRule{{
		Code:     "ORG001",
		Severity: "warning",
		Scope:    "job",
		Field:    "timeout-minutes",
		Equals:   strPtr("360"),
		Message:  "timeout-minutes set to the default",
	}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	p, err := ghparser.Parse("inline.yml", []byte(`
jobs:
  ok:
    runs-on: ubuntu-latest
    timeout-minutes: 15
    steps:
      - run: echo hi
  loophole:
    runs-on: ubuntu-latest
    timeout-minutes: 360
    steps:
      - run: echo hi
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := Check(p, ruleset)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 violation, got %d: %v", len(got), got)
	}
	if got[0].Code != "ORG001" {
		t.Errorf("Code = %q, want ORG001", got[0].Code)
	}
	// "loophole" job starts after "ok"'s 6 lines; just check it's not line 3 (ok's line).
	if got[0].Line == 3 {
		t.Errorf("violation fired on the 'ok' job (line 3) instead of 'loophole'")
	}
}

func TestStepScopeMatchesRegex(t *testing.T) {
	ruleset, err := Compile([]config.CustomRule{{
		Code:     "ORG002",
		Severity: "blocker",
		Scope:    "step",
		Field:    "uses",
		Matches:  strPtr(`^internal-org/`),
		Message:  "must use the vetted internal action mirror",
	}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	p, err := ghparser.Parse("inline.yml", []byte(`
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@11bd71901bbe5b1630ceea73d27597364c9af683
      - uses: internal-org/deploy@11bd71901bbe5b1630ceea73d27597364c9af683
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := Check(p, ruleset)
	if len(got) != 1 || got[0].Code != "ORG002" {
		t.Errorf("expected exactly 1 ORG002 violation, got %v", got)
	}
}

func TestExistsFalseMatchesAbsence(t *testing.T) {
	ruleset, err := Compile([]config.CustomRule{{
		Code:     "ORG003",
		Severity: "info",
		Field:    "runs-on",
		Exists:   boolPtr(false),
		Message:  "job has no runs-on (unexpected in this repo's pattern)",
	}})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	p, err := ghparser.Parse("inline.yml", []byte(`
jobs:
  reusable-call:
    uses: ./.github/workflows/other.yml
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	got := Check(p, ruleset)
	if len(got) != 1 || got[0].Code != "ORG003" {
		t.Errorf("expected exactly 1 ORG003 violation, got %v", got)
	}
}

func TestCompileRejectsInvalidSpecs(t *testing.T) {
	cases := []struct {
		name string
		spec config.CustomRule
	}{
		{"missing code", config.CustomRule{Severity: "warning", Field: "runs-on", Equals: strPtr("x")}},
		{"bad severity", config.CustomRule{Code: "X", Severity: "critical", Field: "runs-on", Equals: strPtr("x")}},
		{"bad scope", config.CustomRule{Code: "X", Severity: "warning", Scope: "workflow", Field: "runs-on", Equals: strPtr("x")}},
		{"unknown field", config.CustomRule{Code: "X", Severity: "warning", Field: "not-a-field", Equals: strPtr("x")}},
		{"no matcher", config.CustomRule{Code: "X", Severity: "warning", Field: "runs-on"}},
		{"bad regex", config.CustomRule{Code: "X", Severity: "warning", Field: "runs-on", Matches: strPtr("(unterminated")}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := Compile([]config.CustomRule{tc.spec}); err == nil {
				t.Error("expected Compile to reject this spec, got nil error")
			}
		})
	}
}
