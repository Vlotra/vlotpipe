//go:build js && wasm

// Command vlotpipe-wasm is the playground entrypoint: no cobra, no file
// I/O, no os/exec — everything the CLI has that a browser sandbox can't
// do. Exposes three JS-callable functions mirroring the CLI's own
// "scan", "--fix", and "format" against a pasted-in string instead of a
// file on disk:
//
//	global.vlotpipeScan(yaml, platform)   -> {violations, error}
//	global.vlotpipeFix(yaml, platform)    -> {fixed, fixedCount, error}
//	global.vlotpipeFormat(yaml)           -> {formatted, changed, error}
//
// Build: GOOS=js GOARCH=wasm go build -o vlotpipe.wasm ./cmd/vlotpipe-wasm
//
// The build tag above is why "go build ./..." on a normal host doesn't
// try (and fail) to compile this package: syscall/js only exists under
// GOOS=js, so without the tag, an ordinary linux/amd64 build of the
// whole module would break on this directory.
package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/vlotra/vlotpipe/internal/fixer"
	"github.com/vlotra/vlotpipe/internal/formatter"
	"github.com/vlotra/vlotpipe/internal/model"
	azureparser "github.com/vlotra/vlotpipe/internal/parser/azure"
	ghparser "github.com/vlotra/vlotpipe/internal/parser/github"
	"github.com/vlotra/vlotpipe/internal/rules"
	_ "github.com/vlotra/vlotpipe/internal/rules/baseline" // registers the rule pack via init()
	"github.com/vlotra/vlotpipe/internal/yamllint"
)

// scanResult, fixResult, and formatResult are the JSON envelopes
// returned to JS — always one fixed shape per function, so the caller
// never needs to branch on success vs. failure before it can read the
// response: a syntax error just means the non-error fields stay at
// their zero value with Error set, not a differently-shaped payload.
type scanResult struct {
	Violations []rules.Violation `json:"violations"`
	Error      string            `json:"error,omitempty"`
}

type fixResult struct {
	Fixed      string `json:"fixed"`
	FixedCount int    `json:"fixedCount"`
	Error      string `json:"error,omitempty"`
}

type formatResult struct {
	Formatted string `json:"formatted"`
	Changed   bool   `json:"changed"`
	Error     string `json:"error,omitempty"`
}

func main() {
	js.Global().Set("vlotpipeScan", js.FuncOf(scan))
	js.Global().Set("vlotpipeFix", js.FuncOf(fix))
	js.Global().Set("vlotpipeFormat", js.FuncOf(format))
	select {} // keep the Go runtime alive; JS calls back into it async
}

// scan(yaml string, platform string) -> JSON string, shape scanResult.
func scan(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return marshal(scanResult{Error: "vlotpipeScan(yaml, platform) requires at least a yaml argument"})
	}
	yamlText, platform := args[0].String(), platformArg(args, 1)

	p, raw, path, err := parseInput(yamlText, platform)
	if err != nil {
		return marshal(scanResult{Error: err.Error()})
	}

	violations := rules.Run(p, nil)
	violations = append(violations, yamllint.Check(path, raw)...)
	return marshal(scanResult{Violations: violations})
}

// fix(yaml string, platform string) -> JSON string, shape fixResult.
// Mirrors "vlotpipe check --fix": runs the rule engine, then applies
// fixer.Fix's safe mechanical subset (TIMEOUT001, AZR001, SEC006,
// LEAN011, PERF002) to whatever it can. Options{} (the zero value)
// means the CLI's own defaults — there's no .vlotpipe.yml in a
// playground paste box to read fix_default/exclude from.
func fix(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return marshalFix(fixResult{Error: "vlotpipeFix(yaml, platform) requires at least a yaml argument"})
	}
	yamlText, platform := args[0].String(), platformArg(args, 1)

	p, raw, _, err := parseInput(yamlText, platform)
	if err != nil {
		return marshalFix(fixResult{Error: err.Error()})
	}

	violations := rules.Run(p, nil)
	fixed, n, ferr := fixer.Fix(p.Platform, raw, violations, fixer.Options{})
	if ferr != nil {
		return marshalFix(fixResult{Error: ferr.Error()})
	}
	return marshalFix(fixResult{Fixed: string(fixed), FixedCount: n})
}

// format(yaml string) -> JSON string, shape formatResult. Mirrors
// "vlotpipe format" (the default surgical indent-only pass, not
// --reorder-keys) — platform-independent, since indent canonicalization
// is purely structural.
func format(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return marshalFormat(formatResult{Error: "vlotpipeFormat(yaml) requires a yaml argument"})
	}
	raw := []byte(args[0].String())

	formatted, err := formatter.FormatIndentOnly(raw)
	if err != nil {
		return marshalFormat(formatResult{Error: err.Error()})
	}
	return marshalFormat(formatResult{Formatted: string(formatted), Changed: string(formatted) != string(raw)})
}

// platformArg reads args[i] if present, defaulting to "github" — every
// entrypoint treats a missing/non-"azure" platform argument as GitHub
// Actions the same way.
func platformArg(args []js.Value, i int) string {
	if len(args) > i {
		return args[i].String()
	}
	return "github"
}

// parseInput dispatches to the right platform parser for yamlText.
// There's no real file path to run Detect() against for pasted text,
// so the caller states the platform explicitly instead; the synthetic
// path returned is only used to shape rule messages/yamllint output the
// same way a real file's path would.
func parseInput(yamlText, platform string) (p *model.Pipeline, raw []byte, path string, err error) {
	raw = []byte(yamlText)
	path = ".github/workflows/playground.yml"
	if platform == "azure" {
		path = "azure-pipelines.yml"
		p, err = azureparser.Parse(path, raw)
	} else {
		p, err = ghparser.Parse(path, raw)
	}
	return p, raw, path, err
}

func marshal(r scanResult) string {
	if r.Violations == nil {
		r.Violations = []rules.Violation{}
	}
	b, err := json.Marshal(r)
	if err != nil {
		// json.Marshal on this shape can't realistically fail (no
		// channels/funcs/cyclic types in rules.Violation), but if it
		// somehow did, a hand-built JSON string is safer than panicking
		// across the js.FuncOf boundary, which crashes the whole page.
		return `{"violations":[],"error":"internal error encoding result"}`
	}
	return string(b)
}

func marshalFix(r fixResult) string {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"fixed":"","fixedCount":0,"error":"internal error encoding result"}`
	}
	return string(b)
}

func marshalFormat(r formatResult) string {
	b, err := json.Marshal(r)
	if err != nil {
		return `{"formatted":"","changed":false,"error":"internal error encoding result"}`
	}
	return string(b)
}
