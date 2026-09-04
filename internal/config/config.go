// Package config loads .vlotpipe.yml: the per-repo exception list that lets
// a team suppress a specific rule on a specific file, with a required
// reason and an optional expiry so suppressions don't silently outlive
// their justification.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const FileName = ".vlotpipe.yml"

// template is what `vlotpipe init` writes. It's deliberately a working,
// commented-out example rather than a bare "ignore: []", so a team can
// see the shape of a real entry instead of having to look it up.
const template = `# vlotpipe exception list — see https://github.com/vlotra/vlotpipe
#
# Two ways to suppress a finding:
#
# 1. Here, for a whole file/pattern, with a reason and (recommended) an
#    expiry so the suppression doesn't silently outlive its justification:
#
# ignore:
#   - code: STRUCT001
#     path: "*"
#     reason: "job naming convention doesn't include test/lint yet, tracked in VLOT-42"
#     expires: "2026-12-31"
#
# 2. Inline, for one line, with a trailing comment in the workflow file
#    itself — no entry needed here:
#
#   secrets: inherit # vlotpipe: ignore[SEC007]
#   uses: some/action@v1 # vlotpipe: ignore

ignore: []

# Custom rules: org-specific policy, no Go code or rebuild required.
# See docs/CUSTOM_RULES.md for the full field list. Example — catch a
# job that satisfies TIMEOUT001 ("a timeout is set") by setting
# timeout-minutes to GitHub's own 360-minute default, which isn't
# actually a bound:
#
# custom_rules:
#   - code: ORG001
#     severity: warning
#     scope: job
#     field: timeout-minutes
#     equals: "360"
#     message: "timeout-minutes is set to the default; pick a real bound"

custom_rules: []

# select: which rule codes/prefixes can fail "check". Empty/omitted
# means every rule can gate the build (today's behavior). "report:" is
# a separate, independent named section — like ruff's separate "lint"
# and "format" config — with its own "select" that defaults to "no
# restriction" on its own; setting the gate above never implicitly
# narrows what's reported. That split is what makes it possible to roll
# a strict CI gate out gradually on an existing repo: gate on a small,
# currently-clean set of rules today, while still seeing every finding
# so you know what to widen the gate to next — rather than either
# failing every open PR on day one, or gating on nothing at all until
# the whole backlog is paid down.
#
# select:
#   - SEC       # every SEC* code
#   - AZR002    # or one specific code
#
# report:
#   select:
#     - SEC
#     - AZR002
#     - TIMEOUT001
#   # Push the complete, unfiltered finding set (not narrowed by either
#   # select above) to a dashboard endpoint after each scan. The URL is
#   # fine to commit; never put a token here — see docs/INTEGRATIONS.md
#   # for the env-var-only auth story (VLOTPIPE_REPORT_TOKEN).
#   to: "https://dashboard.example.com/api/ingest"

# fix: settings for "vlotpipe scan/check --fix" — see docs/rules/README.md
# for which rules have an autofix at all. Both fields are optional.
#
# fix:
#   # Never auto-fix these codes/prefixes, even though they're normally
#   # fixable — the finding still fires and gets reported as usual, only
#   # the automatic edit is skipped. Useful when a team wants
#   # timeout-minutes (or anything else --fix would insert) to stay a
#   # deliberate human decision rather than a value --fix picks for them.
#   exclude:
#     - TIMEOUT001
#     - AZR001
#   # Override the value TIMEOUT001's/AZR001's fix inserts (default: 30).
#   timeout_minutes: 15
`

// Init writes a starter .vlotpipe.yml to dir. It refuses to overwrite an
// existing file unless force is true.
func Init(dir string, force bool) (path string, err error) {
	path = filepath.Join(dir, FileName)
	if !force {
		if _, statErr := os.Stat(path); statErr == nil {
			return path, fmt.Errorf("%s already exists (use --force to overwrite)", path)
		}
	}
	if err := os.WriteFile(path, []byte(template), 0o644); err != nil {
		return path, err
	}
	return path, nil
}

type Ignore struct {
	Code    string `yaml:"code"`
	Path    string `yaml:"path"`
	Reason  string `yaml:"reason"`
	Expires string `yaml:"expires"` // "2006-01-02"; empty means never
}

// CustomRule is a declarative, org-defined policy check: no Go code, no
// rebuild. It matches one field on every job (or every step, if
// scope: step) against a literal value, a negated value, or a regex,
// and reports the given code/message/severity wherever it matches.
type CustomRule struct {
	Code     string `yaml:"code"`
	Severity string `yaml:"severity"` // blocker, warning, or info
	Message  string `yaml:"message"`
	Scope    string `yaml:"scope"` // "job" (default) or "step"
	Field    string `yaml:"field"` // see internal/customrules for the supported field list

	// Pointers so an omitted matcher is distinguishable from one
	// deliberately checking against an empty string.
	Equals    *string `yaml:"equals,omitempty"`
	NotEquals *string `yaml:"not_equals,omitempty"`
	Matches   *string `yaml:"matches,omitempty"` // regex
	Exists    *bool   `yaml:"exists,omitempty"`
}

// ReportConfig groups the settings about what's shown or sent under one
// named section, the way ruff keeps "format" config separate from
// "lint" config rather than flat sibling keys: Select here is
// independent of the top-level Select (the gate) and defaults to "no
// restriction" on its own when omitted, so narrowing the gate alone
// never narrows what's reported.
type ReportConfig struct {
	// Select narrows which rule codes/prefixes appear in scan/check's
	// own output. Empty means every rule is shown (today's behavior).
	// Deliberately does not fall back to the top-level Select: narrowing
	// the gate alone must not narrow what's reported, which is what
	// makes "gate on a small set, but still see everything" possible.
	Select []string `yaml:"select"`
	// To is a URL to push the complete, unfiltered finding set to after
	// a scan (for a fleet-wide dashboard), never narrowed by either
	// Select field. The committed default; --report-to and
	// VLOTPIPE_REPORT_TO both take precedence over this when set. There
	// is deliberately no config-file field for an auth token — see
	// docs/INTEGRATIONS.md.
	To string `yaml:"to"`
}

// FixConfig scopes settings about "--fix" under its own named section,
// same reasoning as ReportConfig: --fix is a distinct concern from
// linting/reporting, with its own defaults, not a flat sibling key.
type FixConfig struct {
	// Exclude lists rule codes/prefixes that "--fix" must never touch,
	// even though they're otherwise in fixer.FixableCodes. Unlike
	// Select/Report.Select, an empty Exclude means "exclude nothing"
	// (fix everything fixable) — the opposite default, because this is
	// a blocklist, not an allowlist. The rule itself keeps firing and
	// getting reported as normal; only the automatic edit is skipped,
	// for a team that wants the finding to stay visible as a prompt for
	// a human decision rather than have --fix quietly pick a value.
	Exclude []string `yaml:"exclude"`
	// TimeoutMinutes overrides the value TIMEOUT001's and AZR001's
	// fixes insert (default 30 — see internal/fixer). 0 (unset) means
	// "use the default."
	TimeoutMinutes int `yaml:"timeout_minutes"`
}

type Config struct {
	Ignores     []Ignore     `yaml:"ignore"`
	CustomRules []CustomRule `yaml:"custom_rules"`
	// MaxStepsPerJob overrides STRUCT002's default step-count threshold.
	// 0 (unset) means "use the default."
	MaxStepsPerJob int `yaml:"max_steps_per_job"`
	// Select narrows which rule codes/prefixes can fail "check". Empty
	// means every rule can gate the build (today's behavior). Independent
	// of Report.Select — see ReportConfig's doc comment.
	Select []string     `yaml:"select"`
	Report ReportConfig `yaml:"report"`
	Fix    FixConfig    `yaml:"fix"`
}

// FixExcluded reports whether code is on the fix.exclude list — the
// inverse of MatchesSelector's "empty means everything," since this is
// a blocklist: empty Fix.Exclude means nothing is excluded.
func (c *Config) FixExcluded(code string) bool {
	for _, e := range c.Fix.Exclude {
		if strings.HasPrefix(code, e) {
			return true
		}
	}
	return false
}

// MatchesSelector reports whether code is selected by selectors: true
// if selectors is empty (no restriction), or if code has any entry in
// selectors as a prefix — "SEC001" matches only that code, "SEC"
// matches every SEC* code, the same convention ruff's own --select
// uses. Shared by Select and Report.Select, which differ only in which
// list is passed, never in how matching works. Exported so a caller
// resolving a list from a CLI flag (rather than from Config directly)
// can apply the identical matching rule.
func MatchesSelector(code string, selectors []string) bool {
	if len(selectors) == 0 {
		return true
	}
	for _, s := range selectors {
		if strings.HasPrefix(code, s) {
			return true
		}
	}
	return false
}

// MatchesSelect reports whether code is allowed to gate the build.
func (c *Config) MatchesSelect(code string) bool {
	return MatchesSelector(code, c.Select)
}

// MatchesReportSelect reports whether code is allowed to appear in
// scan/check's own output.
func (c *Config) MatchesReportSelect(code string) bool {
	return MatchesSelector(code, c.Report.Select)
}

// Load reads dir/.vlotpipe.yml. A missing file is not an error: it just
// means no exceptions are configured.
func Load(dir string) (*Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if os.IsNotExist(err) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// IgnoreFunc returns a predicate suitable for rules.Run: it matches a
// violation's code against path glob c.Ignores, and treats expired entries
// as no longer active so the violation resurfaces.
func (c *Config) IgnoreFunc() func(code, path string) bool {
	now := time.Now()
	return func(code, path string) bool {
		for _, ig := range c.Ignores {
			if ig.Code != code {
				continue
			}
			if ok, _ := filepath.Match(ig.Path, filepath.Base(path)); !ok && ig.Path != path && ig.Path != "*" {
				continue
			}
			if ig.Expires != "" {
				if exp, err := time.Parse("2006-01-02", ig.Expires); err == nil && now.After(exp) {
					continue // expired, so no longer suppressed
				}
			}
			return true
		}
		return false
	}
}
