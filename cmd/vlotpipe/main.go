// Command vlotpipe is a fast, opinionated policy linter for CI pipeline
// definitions (GitHub Actions and Azure Pipelines today, GitLab CI later).
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/vlotra/vlotpipe/internal/config"
	"github.com/vlotra/vlotpipe/internal/customrules"
	"github.com/vlotra/vlotpipe/internal/fixer"
	"github.com/vlotra/vlotpipe/internal/formatter"
	"github.com/vlotra/vlotpipe/internal/model"
	azureparser "github.com/vlotra/vlotpipe/internal/parser/azure"
	ghparser "github.com/vlotra/vlotpipe/internal/parser/github"
	"github.com/vlotra/vlotpipe/internal/pushreport"
	"github.com/vlotra/vlotpipe/internal/report"
	"github.com/vlotra/vlotpipe/internal/rules"
	"github.com/vlotra/vlotpipe/internal/rules/baseline"
	"github.com/vlotra/vlotpipe/internal/rules/repolevel"
	"github.com/vlotra/vlotpipe/internal/yamllint"
)

var (
	flagFormat       string
	flagSeverity     string
	flagConfig       string
	flagFix          bool
	flagStats        bool
	flagInitForce    bool
	flagFormatCheck  bool
	flagSelect       string
	flagReportSelect string
	flagReportTo     string
)

func main() {
	root := &cobra.Command{
		Use:           "vlotpipe [paths...]",
		Short:         "vlotpipe finds risky and wasteful patterns in CI pipeline files",
		Long:          "vlotpipe is a fast, opinionated policy linter for CI pipelines.\nIt scans GitHub Actions workflows and Azure Pipelines files for\nsecurity, structure, performance, and reliability issues.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	scanCmd := &cobra.Command{
		Use:   "scan [paths...]",
		Short: "Scan pipeline files and report every violation (always exits 0)",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := run(args, false, cmd.Flags().Changed("format"))
			return err
		},
	}

	checkCmd := &cobra.Command{
		Use:   "check [paths...]",
		Short: "Scan pipeline files and fail (exit 1) if any blocker-severity violation is found",
		RunE: func(cmd *cobra.Command, args []string) error {
			blockers, err := run(args, true, cmd.Flags().Changed("format"))
			if err != nil {
				return err
			}
			if blockers > 0 {
				os.Exit(1)
			}
			return nil
		},
	}

	initCmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Write a starter " + config.FileName + " and a self-check CI workflow (default: current directory)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			path, err := config.Init(dir, flagInitForce)
			if err != nil {
				return err
			}
			fmt.Println("wrote", path)

			if ciPath, wrote, ciErr := initGitHubWorkflow(dir, flagInitForce); ciErr != nil {
				fmt.Fprintf(os.Stderr, "warning: could not write a CI workflow: %v\n", ciErr)
			} else if wrote {
				fmt.Println("wrote", ciPath)
			} else {
				fmt.Println("skipped", ciPath, "(already exists — use --force to overwrite)")
			}

			if azureSnippet := suggestAzureStep(dir); azureSnippet != "" {
				fmt.Println()
				fmt.Println("This repo also has an azure-pipelines.yml. vlotpipe won't auto-edit an")
				fmt.Println("existing pipeline — add this step yourself where it fits:")
				fmt.Println()
				fmt.Print(azureSnippet)
			}
			return nil
		},
	}
	initCmd.Flags().BoolVar(&flagInitForce, "force", false, "overwrite an existing "+config.FileName)

	formatCmd := &cobra.Command{
		Use:   "format [paths...]",
		Short: "Reformat pipeline files to a consistent style (gofmt-style: total normalization, not a minimal diff)",
		Long: "Reformat pipeline files: canonical key order (workflow/pipeline root, then job,\n" +
			"then step) and a consistent 2-space indent. This is a full parse-and-re-encode\n" +
			"pass, like gofmt for Go — the first run against a hand-formatted file will\n" +
			"produce a large diff, including the loss of blank lines between blocks, which\n" +
			"the underlying YAML representation doesn't track. If that's not the trade-off\n" +
			"you want, \"vlotpipe scan/check --fix\" makes small, targeted edits instead.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runFormat(args, flagFormatCheck)
		},
	}
	formatCmd.Flags().BoolVar(&flagFormatCheck, "check", false, "list files that would be reformatted, without writing; exit 1 if any would change")

	for _, c := range []*cobra.Command{scanCmd, checkCmd} {
		c.Flags().StringVar(&flagFormat, "format", "text", "output format: text, json, github, or azure-devops (auto-detected from the CI environment if omitted)")
		c.Flags().StringVar(&flagSeverity, "severity", "info", "minimum severity to report: blocker, warning, or info")
		c.Flags().StringVar(&flagConfig, "config", "", "directory containing "+config.FileName+" (default: first scanned directory)")
		c.Flags().BoolVar(&flagFix, "fix", false, "auto-fix the subset of violations that have a safe, mechanical fix (see docs/rules/README.md)")
		c.Flags().BoolVarP(&flagStats, "statistics", "s", false, "print an aggregate summary (by severity, by rule, by file) instead of the raw violation list")
		c.Flags().StringVar(&flagSelect, "select", "", "comma-separated rule codes/prefixes that can fail \"check\" (default: every rule can); overrides .vlotpipe.yml's select:")
		c.Flags().StringVar(&flagReportSelect, "report-select", "", "comma-separated rule codes/prefixes to include in output (default: every rule); independent of --select, overrides .vlotpipe.yml's report.select:")
		c.Flags().StringVar(&flagReportTo, "report-to", "", "push the complete, unfiltered finding set to this URL after scanning; overrides VLOTPIPE_REPORT_TO and .vlotpipe.yml's report.to:")
	}

	root.AddCommand(scanCmd, checkCmd, initCmd, formatCmd)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
}

// run discovers pipeline files under paths (default "."), lints them, and
// prints a report. It returns the number of blocker-severity violations
// found so callers can decide whether to fail. formatFlagExplicit is
// whether the caller actually passed --format, distinct from flagFormat
// just holding its "text" default — needed to know whether CI-environment
// auto-detection should override the default at all.
func run(paths []string, ci bool, formatFlagExplicit bool) (int, error) {
	if len(paths) == 0 {
		paths = []string{"."}
	}

	floor := rules.Severity(flagSeverity)
	switch floor {
	case rules.SeverityBlocker, rules.SeverityWarning, rules.SeverityInfo:
	default:
		return 0, fmt.Errorf("invalid --severity %q (want blocker, warning, or info)", flagSeverity)
	}

	cfgDir := flagConfig
	if cfgDir == "" {
		cfgDir = resolveConfigDir(paths[0])
	}
	cfg, err := config.Load(cfgDir)
	if err != nil {
		return 0, fmt.Errorf("loading %s: %w", config.FileName, err)
	}
	ignore := cfg.IgnoreFunc()

	customRuleset, err := customrules.Compile(cfg.CustomRules)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", config.FileName, err)
	}

	// PERF001/LEAN010 read cached_runners through a package-level setter
	// rather than a Check(p) parameter, since both are ordinary
	// registry-based rules (rules.Run only ever passes them a
	// *model.Pipeline) — see ADR 0002 and internal/rules/baseline/cachedrunners.go.
	// Set once per invocation, before rules.Run is called anywhere below.
	for _, code := range []string{"PERF001", "LEAN010"} {
		if runners, ok := cfg.RuleRawStringSlice(code, "cached_runners"); ok {
			baseline.SetCachedRunners(code, runners)
		}
	}
	maxSteps, _ := cfg.RuleRawInt("STRUCT002", "max_steps")
	fixDefaults := map[string]int{}
	for _, code := range []string{"TIMEOUT001", "AZR001"} {
		if v, ok := cfg.RuleRawInt(code, "fix_default"); ok {
			fixDefaults[code] = v
		}
	}

	files, err := discoverWorkflows(paths)
	if err != nil {
		return 0, err
	}

	// everything is every violation found, filtered only by ignore:/inline
	// suppression (already applied inside each Check/Run call below) —
	// deliberately not yet by severity floor or either select list. Three
	// different consumers (local display, check's exit code, the
	// optional report-to push) each need their own differently-scoped
	// view of this same underlying set; deriving all three from one
	// unfiltered collection, after the fact, is what lets them stay
	// independent of each other.
	var everything []rules.Violation
	var pipelines []*model.Pipeline
	fixedTotal := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		p, err := parseFileBytes(f, raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			v := syntaxViolation(f, err)
			if !ignore(v.Code, v.Path) {
				everything = append(everything, v)
			}
			continue
		}

		if flagFix {
			var fixable []rules.Violation
			for _, v := range rules.Run(p, ignore) {
				if !cfg.RuleFixDisabled(v.Code) {
					fixable = append(fixable, v)
				}
			}
			fixed, n, ferr := fixer.Fix(p.Platform, raw, fixable, fixer.Options{TimeoutMinutesByCode: fixDefaults})
			if ferr != nil {
				fmt.Fprintf(os.Stderr, "error: fixing %s: %v\n", f, ferr)
			} else if n > 0 {
				if werr := os.WriteFile(f, fixed, 0o644); werr != nil {
					fmt.Fprintf(os.Stderr, "error: writing %s: %v\n", f, werr)
				} else {
					fixedTotal += n
					raw = fixed
					p, err = parseFileBytes(f, raw)
					if err != nil {
						fmt.Fprintf(os.Stderr, "error: re-parsing %s after fix: %v\n", f, err)
						continue
					}
				}
			}
		}

		pipelines = append(pipelines, p)
		everything = append(everything, rules.Run(p, ignore)...)
		for _, v := range customrules.Check(p, customRuleset) {
			if !ignore(v.Code, v.Path) && !p.IsSuppressed(v.Line, v.Code) {
				everything = append(everything, v)
			}
		}
		for _, v := range baseline.CheckMaxStepsPerJob(p, maxSteps) {
			if !ignore(v.Code, v.Path) && !p.IsSuppressed(v.Line, v.Code) {
				everything = append(everything, v)
			}
		}
		for _, v := range yamllint.Check(f, raw) {
			if !ignore(v.Code, v.Path) && !p.IsSuppressed(v.Line, v.Code) {
				everything = append(everything, v)
			}
		}
	}
	for _, v := range repolevel.CheckDependencyUpdateTooling(cfgDir, pipelines) {
		if !ignore(v.Code, v.Path) {
			everything = append(everything, v)
		}
	}

	// rules.<CODE>.severity (ADR 0002) applies here, once, before
	// everything downstream reads Severity — the floor, select's gate,
	// report.select's display filter, and the report.to push payload
	// all need to see the overridden severity, not just text output's
	// color.
	for i := range everything {
		sev, ok := cfg.RuleSeverity(everything[i].Code)
		if !ok {
			continue
		}
		switch rules.Severity(sev) {
		case rules.SeverityBlocker, rules.SeverityWarning, rules.SeverityInfo:
			everything[i].Severity = rules.Severity(sev)
		}
	}

	if flagFix {
		if fixedTotal > 0 {
			fmt.Fprintf(os.Stderr, "fixed %d violation(s)\n", fixedTotal)
		} else {
			fmt.Fprintln(os.Stderr, "--fix: nothing to fix")
		}
	}

	// --select/--report-select (CLI) take precedence over
	// .vlotpipe.yml's select:/report.select: when given, same
	// override-precedence shape --config already has over the config
	// file's own location.
	selectList := cfg.Select
	if flagSelect != "" {
		selectList = splitCommaList(flagSelect)
	}
	reportSelectList := cfg.Report.Select
	if flagReportSelect != "" {
		reportSelectList = splitCommaList(flagReportSelect)
	}

	// Display: severity floor + report-select. Independent of select
	// (the gate) below — narrowing what can fail the build must never
	// narrow what's shown.
	var all []rules.Violation
	for _, v := range everything {
		if v.Severity.AtLeast(floor) && config.MatchesSelector(v.Code, reportSelectList) {
			all = append(all, v)
		}
	}

	effectiveFormat := flagFormat
	if !formatFlagExplicit {
		// Zero-config annotations: if the caller didn't ask for a
		// specific format, detect the CI system from the environment
		// variable each platform sets on every run and switch to its
		// native annotation format automatically. A "vlotpipe check ."
		// step dropped into an existing workflow starts producing
		// inline PR annotations with no other change required.
		switch {
		case os.Getenv("GITHUB_ACTIONS") == "true":
			effectiveFormat = "github"
		case os.Getenv("TF_BUILD") == "True":
			effectiveFormat = "azure-devops"
		}
	}

	switch effectiveFormat {
	case "json":
		if flagStats {
			if err := report.StatsJSON(os.Stdout, report.BuildStats(all, len(files))); err != nil {
				return 0, err
			}
		} else if err := report.JSON(os.Stdout, all); err != nil {
			return 0, err
		}
	case "github":
		if flagStats {
			return 0, fmt.Errorf("--statistics is not supported with --format github")
		}
		report.GitHub(os.Stdout, all)
	case "azure-devops":
		if flagStats {
			return 0, fmt.Errorf("--statistics is not supported with --format azure-devops")
		}
		report.AzureDevOps(os.Stdout, all)
	case "text", "":
		if flagStats {
			report.StatsText(os.Stdout, report.BuildStats(all, len(files)))
		} else {
			report.Text(os.Stdout, all, len(files))
		}
	default:
		return 0, fmt.Errorf("invalid --format %q (want text, json, github, or azure-devops)", flagFormat)
	}

	// Gate: select narrows *which codes* can fail the build; blocker
	// severity still decides *whether* a match actually gates, unchanged
	// from before select existed. select doesn't let a non-blocker gate
	// the build, and is independent of reportSelectList/floor above —
	// narrowing what's displayed must never narrow what gates, either.
	blockers := 0
	for _, v := range everything {
		if v.Severity == rules.SeverityBlocker && config.MatchesSelector(v.Code, selectList) {
			blockers++
		}
	}

	// Push: always the complete, unfiltered set — never narrowed by
	// select or report-select, since full visibility regardless of any
	// local scoping is the entire point of a dashboard push existing.
	// Best-effort: a failed push is reported but never changes the exit
	// code or fails the scan itself.
	reportTo := firstNonEmpty(flagReportTo, os.Getenv("VLOTPIPE_REPORT_TO"), cfg.Report.To)
	if reportTo != "" {
		repo, branch, commit := pushreport.GitMetadata(cfgDir)
		payload := pushreport.Payload{
			Repo:         repo,
			Branch:       branch,
			Commit:       commit,
			ScannedAt:    time.Now().UTC(),
			FilesScanned: len(files),
			Violations:   everything,
		}
		if err := pushreport.Push(reportTo, os.Getenv("VLOTPIPE_REPORT_TOKEN"), payload); err != nil {
			fmt.Fprintf(os.Stderr, "warning: report_to push failed: %v\n", err)
		}
	}

	_ = ci
	return blockers, nil
}

// firstNonEmpty returns the first non-empty string among vals, or "" if
// all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// splitCommaList splits a comma-separated --select/--report-select flag
// value into a trimmed list, e.g. "SEC, AZR002" -> ["SEC", "AZR002"].
func splitCommaList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// runFormat discovers pipeline files under paths (default ".") and
// reformats each one in place (or, with check, only reports which ones
// would change). Returns an error only for a hard failure (a path that
// doesn't exist); a file that would be reformatted under --check is
// reported via os.Exit(1), matching gofmt -l's convention of a
// non-error, non-zero exit rather than a Go error value.
func runFormat(paths []string, check bool) error {
	if len(paths) == 0 {
		paths = []string{"."}
	}
	files, err := discoverWorkflows(paths)
	if err != nil {
		return err
	}

	changed := 0
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		platform := model.PlatformAzurePipelines
		if ghparser.Detect(f) {
			platform = model.PlatformGitHubActions
		}
		formatted, err := formatter.Format(platform, raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: formatting %s: %v\n", f, err)
			continue
		}
		if bytes.Equal(raw, formatted) {
			continue
		}
		changed++
		if check {
			fmt.Println(f)
			continue
		}
		if err := os.WriteFile(f, formatted, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "error: writing %s: %v\n", f, err)
		}
	}

	if check {
		if changed > 0 {
			os.Exit(1)
		}
		return nil
	}
	fmt.Fprintf(os.Stderr, "formatted %d file(s), %d already formatted\n", changed, len(files)-changed)
	return nil
}

// skipDirs are vendored/generated directories that are never the repo
// owner's own CI to fix — a workflow file inside node_modules belongs to
// a dependency, not the project being scanned.
var skipDirs = map[string]bool{
	"node_modules": true, "vendor": true, ".venv": true, "venv": true,
	".git": true, "dist": true, "build": true, ".next": true,
	"target": true, "bower_components": true, ".pnpm": true,
}

// isPipelineFile reports whether path is a pipeline definition file for
// any supported platform.
func isPipelineFile(path string) bool {
	return ghparser.Detect(path) || azureparser.Detect(path)
}

// parseFileBytes dispatches to the right platform parser based on which
// platform's Detect matched, so callers don't need to duplicate the
// platform-detection logic discoverWorkflows already ran. Takes the raw
// bytes directly (rather than re-reading path) so a caller that also
// needs the raw content — as --fix does, to hand to fixer.Fix — reads
// the file exactly once.
func parseFileBytes(path string, data []byte) (*model.Pipeline, error) {
	if ghparser.Detect(path) {
		return ghparser.Parse(path, data)
	}
	return azureparser.Parse(path, data)
}

// selfCheckWorkflowTemplate is what "vlotpipe init" writes as the repo's
// own CI gate — and, being a real .github/workflows/*.yml file, is
// itself something "vlotpipe scan" would lint. It's written to actually
// pass that scan, not just to exist: actions/checkout is pinned to
// 08eba0b27e820071cde6df949e0beb9ba4906955 (v7.0.1, verified against the
// GitHub API when this was written, not guessed), has a timeout,
// persist-credentials: false, and a concurrency group. The one
// exception is SEC001 on the vlotra/vlotpipe step itself: it can't be
// pinned to a commit SHA because this repo has no public remote or
// tagged release yet to resolve one from — suppressed inline with the
// reason on record, rather than silently failing vlotpipe's own SEC001,
// the same way any real repo would document a deliberate exception.
const selfCheckWorkflowTemplate = `name: vlotpipe
on:
  push:
    branches: [main]
  pull_request:

permissions:
  contents: read

concurrency:
  group: ${{ github.workflow }}-${{ github.ref }}
  cancel-in-progress: true

jobs:
  lint-pipelines:
    runs-on: ubuntu-latest
    timeout-minutes: 10
    steps:
      - uses: actions/checkout@08eba0b27e820071cde6df949e0beb9ba4906955 # v7.0.1
        with:
          persist-credentials: false
      - uses: vlotra/vlotpipe@main # vlotpipe: ignore[SEC001] no tagged release exists yet; pin to one once it does
        with:
          fail-on: blocker
`

// initGitHubWorkflow writes dir/.github/workflows/vlotpipe.yml — the
// repo's own self-check gate — unless it already exists and force is
// false, in which case it's skipped (not an error): the primary
// deliverable of "init" is .vlotpipe.yml, already written by the time
// this runs, and a coincidentally-pre-existing workflow file with this
// name shouldn't block that.
func initGitHubWorkflow(dir string, force bool) (path string, wrote bool, err error) {
	wfDir := filepath.Join(dir, ".github", "workflows")
	path = filepath.Join(wfDir, "vlotpipe.yml")
	if !force {
		if _, statErr := os.Stat(path); statErr == nil {
			return path, false, nil
		}
	}
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		return path, false, err
	}
	if err := os.WriteFile(path, []byte(selfCheckWorkflowTemplate), 0o644); err != nil {
		return path, false, err
	}
	return path, true, nil
}

// suggestAzureStep returns a ready-to-paste Azure Pipelines step if dir
// has an azure-pipelines.yml at its root, or "" otherwise. Unlike the
// GitHub Actions case, an Azure pipeline is one monolithic file
// defining the whole build — auto-writing or auto-merging into it risks
// silently replacing or corrupting whatever real pipeline is already
// there, so this only ever prints a snippet for a human to place, never
// edits the file itself.
func suggestAzureStep(dir string) string {
	if _, err := os.Stat(filepath.Join(dir, "azure-pipelines.yml")); err != nil {
		return ""
	}
	return `  - task: GoTool@0
    inputs:
      version: '1.26'
  - script: go install github.com/vlotra/vlotpipe/cmd/vlotpipe@latest
    displayName: 'Install vlotpipe'
  - script: vlotpipe check .
    displayName: 'Lint CI pipelines'
`
}

// resolveConfigDir finds the directory .vlotpipe.yml should be loaded
// from when --config wasn't given explicitly, starting at anchor (the
// first scanned path, which may be a file or a directory) and walking
// upward toward the filesystem root.
//
// A plain "use anchor's own directory" isn't enough on its own: a
// pre-commit hook or a CI step invokes vlotpipe with individual staged
// file paths like ".github/workflows/ci.yml", whose *containing*
// directory (.github/workflows/) is never where .vlotpipe.yml actually
// lives — the repo root is, several levels up. So this walks upward
// from wherever scanning started, the same way git/eslint/prettier find
// their own config, stopping at the first directory that either has a
// .vlotpipe.yml or looks like the repo root (a ".git" entry) — past
// that point, an ancestor directory almost certainly belongs to
// something else, and walking further risks picking up a stray
// .vlotpipe.yml higher up the filesystem that has nothing to do with
// this repo. Finding nothing falls back to anchor's own directory,
// matching config.Load's existing "missing file is not an error"
// behavior — scanning proceeds with no exceptions configured.
func resolveConfigDir(anchor string) string {
	start := anchor
	if info, err := os.Stat(anchor); err == nil && !info.IsDir() {
		start = filepath.Dir(anchor)
	}
	dir := start
	for {
		if _, err := os.Stat(filepath.Join(dir, config.FileName)); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return start
		}
		dir = parent
	}
}

// yamlErrorLine matches gopkg.in/yaml.v3's own error message shape,
// "yaml: line N: <what went wrong>", wherever it appears in the error
// text — both parser packages wrap it as "<path>: yaml: line N: ..."
// via fmt.Errorf("%s: %w", ...), so this must not anchor to the start
// of the string. Confirmed empirically that N is 0-indexed relative to
// the file's own 1-indexed physical line numbers (a tab on physical
// line 8 reports "line 7"; an unclosed "{" starting on physical line 7
// reports "line 6") — so the reported number always needs +1 to point
// at the actual offending line.
var yamlErrorLine = regexp.MustCompile(`yaml: line (\d+):`)

// syntaxViolation turns a YAML parse failure into a blocker-severity
// Violation instead of a silent stderr line and a skipped file. Before
// this, a pipeline file with genuinely broken YAML — a stray tab, an
// unclosed flow mapping — printed an error to stderr but "vlotpipe
// check" still exited 0 and printed "All checks passed!", exactly the
// case a CI gate most needs to catch.
func syntaxViolation(path string, err error) rules.Violation {
	line := 1
	if m := yamlErrorLine.FindStringSubmatch(err.Error()); m != nil {
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			line = n + 1
		}
	}
	// err.Error() already starts with "<path>: " (both parsers wrap it
	// that way) — Path is reported separately by every output format,
	// so strip that prefix rather than showing the path twice.
	msg := strings.TrimPrefix(err.Error(), path+": ")
	return rules.Violation{
		Code:     "YAML001",
		Rule:     "invalid-yaml",
		Severity: rules.SeverityBlocker,
		Message:  "invalid YAML: " + msg,
		Path:     path,
		Line:     line,
		Col:      1,
	}
}

// discoverWorkflows walks paths and returns every pipeline definition
// file (any supported platform) found beneath them, skipping
// vendored/generated directories.
func discoverWorkflows(paths []string) ([]string, error) {
	var files []string
	for _, root := range paths {
		info, err := os.Stat(root)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			if isPipelineFile(root) {
				files = append(files, root)
			}
			continue
		}
		err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				if skipDirs[info.Name()] {
					return filepath.SkipDir
				}
				// A nested git submodule's ".git" is a gitlink file (not a
				// directory) pointing at the parent repo's .git/modules/.
				// Its workflows belong to the submodule's own upstream repo,
				// not to whatever the user pointed vlotpipe at — unless this
				// directory *is* the scan root, which the user chose deliberately.
				if path != root {
					if gitInfo, gitErr := os.Lstat(filepath.Join(path, ".git")); gitErr == nil && !gitInfo.IsDir() {
						return filepath.SkipDir
					}
				}
				return nil
			}
			if isPipelineFile(path) {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return files, nil
}
