//go:build js && wasm

// Command vlotpipe-wasm is the playground entrypoint: no cobra, no file
// I/O, no os/exec — everything the CLI has that a browser sandbox can't
// do. Exposes one JS-callable function, global.vlotpipeScan(yaml,
// platform), that runs the same parser + rule engine as "vlotpipe scan"
// against a pasted-in string instead of a file on disk.
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

	"github.com/vlotra/vlotpipe/internal/model"
	azureparser "github.com/vlotra/vlotpipe/internal/parser/azure"
	ghparser "github.com/vlotra/vlotpipe/internal/parser/github"
	"github.com/vlotra/vlotpipe/internal/rules"
	_ "github.com/vlotra/vlotpipe/internal/rules/baseline" // registers the rule pack via init()
	"github.com/vlotra/vlotpipe/internal/yamllint"
)

// result is the JSON envelope returned to JS — always this one shape, so
// the caller never needs to branch on success vs. failure before it can
// read the response: a syntax error just means an empty Violations with
// Error set, not a differently-shaped payload.
type result struct {
	Violations []rules.Violation `json:"violations"`
	Error      string            `json:"error,omitempty"`
}

func main() {
	js.Global().Set("vlotpipeScan", js.FuncOf(scan))
	select {} // keep the Go runtime alive; JS calls back into it async
}

// scan(yaml string, platform string) -> JSON string, shape `result`.
// platform is "azure" for Azure Pipelines, anything else (including
// omitted) defaults to GitHub Actions — there's no real file path to
// run Detect() against for pasted text, so the caller states it
// explicitly instead.
func scan(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return marshal(result{Error: "vlotpipeScan(yaml, platform) requires at least a yaml argument"})
	}
	yamlText := args[0].String()
	platform := "github"
	if len(args) > 1 {
		platform = args[1].String()
	}

	raw := []byte(yamlText)
	path := ".github/workflows/playground.yml"
	if platform == "azure" {
		path = "azure-pipelines.yml"
	}

	var (
		p   *model.Pipeline
		err error
	)
	if platform == "azure" {
		p, err = azureparser.Parse(path, raw)
	} else {
		p, err = ghparser.Parse(path, raw)
	}
	if err != nil {
		return marshal(result{Error: err.Error()})
	}

	violations := rules.Run(p, nil)
	violations = append(violations, yamllint.Check(path, raw)...)
	return marshal(result{Violations: violations})
}

func marshal(r result) string {
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
