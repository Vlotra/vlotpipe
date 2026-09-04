package yamllint

import "testing"

type codeMsg struct{ code, msg string }

func toCodeMsg(t *testing.T, raw []byte) []codeMsg {
	t.Helper()
	var out []codeMsg
	for _, v := range Check("f.yml", raw) {
		out = append(out, codeMsg{v.Code, v.Message})
	}
	return out
}

func has(t *testing.T, raw []byte, code string) bool {
	t.Helper()
	for _, v := range toCodeMsg(t, raw) {
		if v.code == code {
			return true
		}
	}
	return false
}

func TestDuplicateKeyFires(t *testing.T) {
	src := []byte("jobs:\n  build:\n    runs-on: ubuntu-latest\n    runs-on: windows-latest\n")
	if !has(t, src, "YAML002") {
		t.Errorf("expected YAML002 to fire on a duplicate key, violations: %v", toCodeMsg(t, src))
	}
}

func TestDuplicateKeyDoesNotFireAcrossDifferentMappings(t *testing.T) {
	src := []byte("jobs:\n  a:\n    runs-on: ubuntu-latest\n  b:\n    runs-on: ubuntu-latest\n")
	if has(t, src, "YAML002") {
		t.Errorf("same key name in two different jobs must not count as a duplicate")
	}
}

func TestDuplicateKeyReportsSecondOccurrenceLine(t *testing.T) {
	src := []byte("a: 1\nb: 2\na: 3\n")
	var found bool
	for _, v := range Check("f.yml", src) {
		if v.Code == "YAML002" {
			found = true
			if v.Line != 3 {
				t.Errorf("Line = %d, want 3 (the second occurrence)", v.Line)
			}
		}
	}
	if !found {
		t.Fatal("YAML002 did not fire")
	}
}

func TestTrailingWhitespaceFires(t *testing.T) {
	src := []byte("jobs:\n  build:   \n    runs-on: ubuntu-latest\n")
	if !has(t, src, "YAML003") {
		t.Errorf("expected YAML003 on a line with trailing spaces, violations: %v", toCodeMsg(t, src))
	}
}

func TestTrailingWhitespaceCleanFileIsQuiet(t *testing.T) {
	src := []byte("jobs:\n  build:\n    runs-on: ubuntu-latest\n")
	if has(t, src, "YAML003") {
		t.Errorf("did not expect YAML003 on a file with no trailing whitespace")
	}
}

func TestFinalNewlineMissingFires(t *testing.T) {
	src := []byte("jobs:\n  build:\n    runs-on: ubuntu-latest")
	if !has(t, src, "YAML004") {
		t.Errorf("expected YAML004 when the file has no trailing newline")
	}
}

func TestFinalNewlineExtraBlankLinesFires(t *testing.T) {
	src := []byte("jobs:\n  build:\n    runs-on: ubuntu-latest\n\n\n")
	if !has(t, src, "YAML004") {
		t.Errorf("expected YAML004 when the file ends with extra blank lines")
	}
}

func TestFinalNewlineExactlyOneIsQuiet(t *testing.T) {
	src := []byte("jobs:\n  build:\n    runs-on: ubuntu-latest\n")
	if has(t, src, "YAML004") {
		t.Errorf("did not expect YAML004 on a file ending in exactly one newline")
	}
}

func TestTruthyValueFiresOnAmbiguousSpellings(t *testing.T) {
	cases := []string{"yes", "Yes", "YES", "no", "No", "on", "off", "y", "n"}
	for _, word := range cases {
		t.Run(word, func(t *testing.T) {
			src := []byte("jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: some/action@v1\n        with:\n          enabled: " + word + "\n")
			if !has(t, src, "YAML005") {
				t.Errorf("expected YAML005 to fire for unquoted %q, violations: %v", word, toCodeMsg(t, src))
			}
		})
	}
}

func TestTruthyValueDoesNotFireOnTrueFalse(t *testing.T) {
	cases := []string{"true", "false", "True", "False"}
	for _, word := range cases {
		t.Run(word, func(t *testing.T) {
			src := []byte("jobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - uses: some/action@v1\n        with:\n          enabled: " + word + "\n")
			if has(t, src, "YAML005") {
				t.Errorf("did not expect YAML005 for %q — true/false is the normal, correct way to write a boolean", word)
			}
		})
	}
}

func TestTruthyValueDoesNotFireWhenQuoted(t *testing.T) {
	src := []byte(`jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: some/action@v1
        with:
          enabled: "yes"
`)
	if has(t, src, "YAML005") {
		t.Errorf("a quoted 'yes' is an unambiguous literal string, must not fire")
	}
}

func TestTruthyValueNeverFiresOnTheOnKey(t *testing.T) {
	// GitHub Actions' own trigger key. yamllint's default config
	// famously flags this; the whole point of scanning only *values*
	// (never keys) is that this never becomes a candidate.
	src := []byte("on: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n")
	if has(t, src, "YAML005") {
		t.Errorf("the 'on:' key itself must never be flagged, only ambiguous values")
	}
}

func TestCheckReturnsNothingOnUnparseableInput(t *testing.T) {
	// A syntax error is YAML001's job (in cmd/vlotpipe), not this
	// package's — Check must degrade gracefully, not panic, and must
	// still report the byte-level checks (trailing whitespace, final
	// newline) that don't require a valid tree.
	src := []byte("jobs: [unclosed\n")
	got := Check("f.yml", src)
	for _, v := range got {
		if v.Code == "YAML002" || v.Code == "YAML005" {
			t.Errorf("tree-based check %s fired on unparseable input, should have been skipped", v.Code)
		}
	}
}

func TestCleanFileProducesNoFindings(t *testing.T) {
	src := []byte("name: CI\non: push\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo hi\n")
	if got := Check("f.yml", src); len(got) != 0 {
		t.Errorf("expected a clean file to produce no findings, got %v", got)
	}
}
