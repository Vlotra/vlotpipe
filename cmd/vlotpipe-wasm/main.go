//go:build js && wasm

// Command vlotpipe-wasm is the playground entrypoint: no cobra, no file
// I/O, no os/exec — everything the CLI has that a browser sandbox can't
// do. Exposes JS-callable functions mirroring the CLI's own "scan",
// "--fix", and "format" against pasted-in or uploaded content instead
// of files on disk:
//
//	global.vlotpipeScan(yaml, platform, configYAML?)
//	    -> {filesScanned, violations, blockers, clusters, configError, error}
//	global.vlotpipeScanFiles(filesJSON, configYAML?)
//	    -> same shape, filesJSON is a JSON array of {path, content}
//	global.vlotpipeFix(yaml, platform, configYAML?) -> {fixed, fixedCount, error}
//	global.vlotpipeFormat(yaml)                     -> {formatted, changed, error}
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
	"strings"
	"syscall/js"

	"github.com/vlotra/vlotpipe/internal/config"
	"github.com/vlotra/vlotpipe/internal/fingerprint"
	"github.com/vlotra/vlotpipe/internal/fixer"
	"github.com/vlotra/vlotpipe/internal/formatter"
	"github.com/vlotra/vlotpipe/internal/model"
	azureparser "github.com/vlotra/vlotpipe/internal/parser/azure"
	ghparser "github.com/vlotra/vlotpipe/internal/parser/github"
	"github.com/vlotra/vlotpipe/internal/rules"
	_ "github.com/vlotra/vlotpipe/internal/rules/baseline" // registers the rule pack via init()
	"github.com/vlotra/vlotpipe/internal/yamllint"
)

// fileInput is one uploaded/pasted file, as sent from JS.
type fileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// scanResult is the JSON envelope every scan-shaped function returns —
// one fixed shape, so the caller never needs to branch on success vs.
// failure before it can read the response. Violations is already
// filtered by ignore: and report.select:, matching what "vlotpipe
// scan" prints; Blockers is the select:-gated count, matching what
// "vlotpipe check"'s exit code would be based on — a config with a
// narrow select: can show Blockers=0 even though Violations is
// non-empty, same as the real CLI's gradual-adoption story.
type scanResult struct {
	FilesScanned int               `json:"filesScanned"`
	Violations   []rules.Violation `json:"violations"`
	Blockers     int               `json:"blockers"`
	// Clusters deliberately has no ",omitempty" — encoding/json treats
	// omitempty on a slice as "omit if len == 0", not just "omit if
	// nil", so keeping it would drop the field entirely on the (very
	// common) no-duplicates case instead of encoding "[]". JS callers
	// can then always read result.clusters.length without a null
	// check, same as they already can for .violations.
	Clusters []fingerprint.Group `json:"clusters"`
	// ConfigError is set when configYAML was non-empty but failed to
	// parse — the scan still proceeds with an unconfigured (identity)
	// config rather than failing outright, since a config typo
	// shouldn't block seeing scan results at all.
	ConfigError string `json:"configError,omitempty"`
	Error       string `json:"error,omitempty"`
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
	js.Global().Set("vlotpipeScanFiles", js.FuncOf(scanFilesJS))
	js.Global().Set("vlotpipeFix", js.FuncOf(fix))
	js.Global().Set("vlotpipeFormat", js.FuncOf(format))
	select {} // keep the Go runtime alive; JS calls back into it async
}

// scan(yaml string, platform string, configYAML? string) -> JSON
// string, shape scanResult. platform is "azure" for Azure Pipelines,
// anything else (including omitted) defaults to GitHub Actions —
// there's no real file path to run Detect() against for pasted text,
// so the caller states it explicitly, and this synthesizes a path
// that Detect() would itself recognize for that platform.
func scan(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return marshal(scanResult{Error: "vlotpipeScan(yaml, platform, configYAML?) requires at least a yaml argument"})
	}
	platform := platformArg(args, 1)
	path := ".github/workflows/playground.yml"
	if platform == "azure" {
		path = "azure-pipelines.yml"
	}
	files := []fileInput{{Path: path, Content: args[0].String()}}
	return marshal(scanFiles(files, configArg(args, 2)))
}

// scanFilesJS(filesJSON string, configYAML? string) -> JSON string,
// shape scanResult. filesJSON is a JSON array of {path, content} —
// path is what decides platform (via the same Detect() the CLI uses
// to walk a real directory), and any file neither parser recognizes
// is silently skipped rather than erroring the whole batch, the same
// as "vlotpipe scan ." walking a directory that also contains
// non-pipeline files.
func scanFilesJS(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return marshal(scanResult{Error: "vlotpipeScanFiles(filesJSON, configYAML?) requires a filesJSON argument"})
	}
	var files []fileInput
	if err := json.Unmarshal([]byte(args[0].String()), &files); err != nil {
		return marshal(scanResult{Error: "couldn't parse the file list: " + err.Error()})
	}
	return marshal(scanFiles(files, configArg(args, 1)))
}

// scanFiles is the shared core behind both scan entrypoints: parse
// every recognized file, run the rule engine + yamllint on each
// (respecting config's ignore:), then derive the same three
// independent views cmd/vlotpipe/main.go's run() does — displayed
// (report.select:-filtered), gate (select:-filtered blocker count),
// and duplicate clusters (DUP001-suppression-aware) — from one
// underlying ignore-filtered violation set.
func scanFiles(files []fileInput, configYAML string) scanResult {
	cfg, cfgErr := parseConfig(configYAML)
	ignore := cfg.IgnoreFunc()

	var everything []rules.Violation
	var pipelines []*model.Pipeline
	var parseErrs []string
	filesScanned := 0

	for _, f := range files {
		raw := []byte(f.Content)
		var (
			p   *model.Pipeline
			err error
		)
		switch {
		case azureparser.Detect(f.Path):
			p, err = azureparser.Parse(f.Path, raw)
		case ghparser.Detect(f.Path):
			p, err = ghparser.Parse(f.Path, raw)
		default:
			continue // not a pipeline file — skip silently, same as a directory walk would
		}
		if err != nil {
			parseErrs = append(parseErrs, f.Path+": "+err.Error())
			continue
		}
		filesScanned++
		pipelines = append(pipelines, p)
		everything = append(everything, rules.Run(p, ignore)...)
		for _, v := range yamllint.Check(f.Path, raw) {
			if !ignore(v.Code, v.Path) {
				everything = append(everything, v)
			}
		}
	}

	var displayed []rules.Violation
	blockers := 0
	for _, v := range everything {
		if cfg.MatchesReportSelect(v.Code) {
			displayed = append(displayed, v)
		}
		if v.Severity == rules.SeverityBlocker && cfg.MatchesSelect(v.Code) {
			blockers++
		}
	}

	chunks := fingerprint.BuildChunks(pipelines)
	kept := chunks[:0]
	for _, c := range chunks {
		if !ignore(fingerprint.Code, c.Path) {
			kept = append(kept, c)
		}
	}
	clusters := fingerprint.Cluster(kept, fingerprint.DefaultThreshold)

	return scanResult{
		FilesScanned: filesScanned,
		Violations:   displayed,
		Blockers:     blockers,
		Clusters:     clusters,
		ConfigError:  cfgErr,
		Error:        strings.Join(parseErrs, "; "),
	}
}

// fix(yaml string, platform string, configYAML? string) -> JSON
// string, shape fixResult. Mirrors "vlotpipe check --fix": runs the
// rule engine (config's ignore: already applied), skips any code
// config's fix.exclude: disabled, then applies fixer.Fix's safe
// mechanical subset (TIMEOUT001, AZR001, SEC006, LEAN011, PERF002) —
// fix.timeout_minutes overrides TIMEOUT001/AZR001's inserted value the
// same way it does on the CLI.
func fix(_ js.Value, args []js.Value) any {
	if len(args) < 1 {
		return marshalFix(fixResult{Error: "vlotpipeFix(yaml, platform, configYAML?) requires at least a yaml argument"})
	}
	yamlText, platform := args[0].String(), platformArg(args, 1)
	cfg, _ := parseConfig(configArg(args, 2))

	p, raw, _, err := parseSingle(yamlText, platform)
	if err != nil {
		return marshalFix(fixResult{Error: err.Error()})
	}

	ignore := cfg.IgnoreFunc()
	var fixable []rules.Violation
	for _, v := range rules.Run(p, ignore) {
		if !cfg.RuleFixDisabled(v.Code) {
			fixable = append(fixable, v)
		}
	}
	timeouts := map[string]int{}
	for _, code := range []string{"TIMEOUT001", "AZR001"} {
		if v, ok := cfg.RuleRawInt(code, "fix_default"); ok {
			timeouts[code] = v
		}
	}

	fixed, n, ferr := fixer.Fix(p.Platform, raw, fixable, fixer.Options{TimeoutMinutesByCode: timeouts})
	if ferr != nil {
		return marshalFix(fixResult{Error: ferr.Error()})
	}
	return marshalFix(fixResult{Fixed: string(fixed), FixedCount: n})
}

// format(yaml string) -> JSON string, shape formatResult. Mirrors
// "vlotpipe format" (the default surgical indent-only pass, not
// --reorder-keys) — platform-independent, since indent canonicalization
// is purely structural, and config-independent, since nothing in
// .vlotpipe.yml affects formatting.
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

// configArg reads args[i] (the optional trailing configYAML argument)
// if present, else "".
func configArg(args []js.Value, i int) string {
	if len(args) > i {
		return args[i].String()
	}
	return ""
}

// parseConfig parses configYAML if non-empty, falling back to an
// unconfigured (identity) *config.Config on either an empty input or a
// parse error — a config typo degrades to "no config" rather than
// blocking the scan outright, with the error surfaced separately
// (ConfigError) so the UI can show it without losing scan results.
func parseConfig(configYAML string) (*config.Config, string) {
	if strings.TrimSpace(configYAML) == "" {
		return &config.Config{}, ""
	}
	cfg, err := config.Parse([]byte(configYAML))
	if err != nil {
		return &config.Config{}, err.Error()
	}
	return cfg, ""
}

// parseSingle dispatches yamlText to the right platform parser for
// fix() — same synthetic-path trick scan() uses.
func parseSingle(yamlText, platform string) (p *model.Pipeline, raw []byte, path string, err error) {
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
	if r.Clusters == nil {
		r.Clusters = []fingerprint.Group{}
	}
	b, err := json.Marshal(r)
	if err != nil {
		// json.Marshal on this shape can't realistically fail (no
		// channels/funcs/cyclic types in these structs), but if it
		// somehow did, a hand-built JSON string is safer than
		// panicking across the js.FuncOf boundary, which crashes the
		// whole page.
		return `{"filesScanned":0,"violations":[],"blockers":0,"error":"internal error encoding result"}`
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
