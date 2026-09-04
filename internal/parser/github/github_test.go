package github

import "testing"

func TestParseSuppressionsBareIgnore(t *testing.T) {
	p, err := Parse("inline.yml", []byte(`
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4 # vlotpipe: ignore
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	line := p.Jobs[0].Steps[0].Line
	if !p.IsSuppressed(line, "SEC001") || !p.IsSuppressed(line, "ANYTHING") {
		t.Errorf("expected a bare ignore comment to suppress every code on line %d", line)
	}
}

func TestParseSuppressionsSpecificCodes(t *testing.T) {
	p, err := Parse("inline.yml", []byte(`
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    secrets: inherit # vlotpipe: ignore[SEC007, SEC008]
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	line := p.Jobs[0].SecretsLine
	if !p.IsSuppressed(line, "SEC007") {
		t.Errorf("expected SEC007 to be suppressed on line %d", line)
	}
	if !p.IsSuppressed(line, "SEC008") {
		t.Errorf("expected SEC008 to be suppressed on line %d", line)
	}
	if p.IsSuppressed(line, "SEC005") {
		t.Errorf("expected SEC005 to NOT be suppressed on line %d", line)
	}
}

func TestParseSuppressionsUnrelatedCommentDoesNothing(t *testing.T) {
	p, err := Parse("inline.yml", []byte(`
jobs:
  build:
    runs-on: ubuntu-latest # just a normal comment
    steps:
      - run: echo hi
`))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(p.Suppressions) != 0 {
		t.Errorf("expected no suppressions from an unrelated comment, got %v", p.Suppressions)
	}
}
