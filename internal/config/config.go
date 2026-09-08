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

# rules: per-code configuration, keyed by exact rule code (never a
# category prefix — unlike select/report.select above, which do prefix
# match). Two generic properties every rule accepts (severity, fix);
# everything else is rule-specific and documented on that rule's own
# docs/rules/<CODE>.md page. See docs/adr/0002-rule-specific-runner-config.md
# for the full reasoning.
#
# rules:
#   SEC001:
#     severity: warning       # replace this code's shipped severity —
#                              # applies everywhere: display, the
#                              # check gate, and the report.to push.
#   STRUCT002:
#     max_steps: 30            # STRUCT002-specific: the step-count threshold
#   PERF001:
#     cached_runners: ["gha-hmak-web", "*"]  # exact label or "*"; a
#       # runner on this list is known to already have persistent
#       # caching, so PERF001 never fires for a job on it at all.
#   LEAN010:
#     cached_runners: ["gha-hmak-web"]
#   TIMEOUT001:
#     fix: false                # still fires and gets reported — just
#                                # never auto-fixed by --fix
#     fix_default: 15           # override the value --fix inserts (default: 30)
#   AZR001:
#     fix_default: 15
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

// UnmarshalYAML lets an ignore: entry be a bare code string, shorthand
// for {code: <string>, path: "*"} with no reason/expiry, alongside the
// full object form. "ignore: [SEC001, TIMEOUT001]" is the obvious thing
// to type for "suppress these everywhere" — before this, it failed with
// a raw "cannot unmarshal !!str `SEC001` into config.Ignore" instead of
// working. Reason/expiry are only ever documented as recommended, never
// required, so the shorthand's silent "no reason" isn't a new gap.
func (ig *Ignore) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		ig.Code = node.Value
		ig.Path = "*"
		return nil
	}
	type plain Ignore
	var p plain
	if err := node.Decode(&p); err != nil {
		return err
	}
	*ig = Ignore(p)
	return nil
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

// RuleConfig is one rule code's entry under the rules: section (ADR
// 0002): two generic properties meaningful for every rule (Severity,
// Fix), plus Raw — everything else in that code's mapping, for rules
// with their own special properties (STRUCT002's max_steps,
// PERF001/LEAN010's cached_runners, TIMEOUT001/AZR001's fix_default).
// Raw exists because special properties differ per rule; a single flat
// struct with a field for every rule's every special property doesn't
// scale the way the two generic ones do, so each rule that wants one
// reads it out of Raw itself, by name, with its own fallback on
// absence or a wrong type.
type RuleConfig struct {
	// Severity replaces this code's shipped severity everywhere it's
	// consulted: display, the check gate, and the report.to push —
	// never just a display-only recolor. nil means "use the rule's own
	// default."
	Severity *string
	// Fix, when explicitly false, skips this code's automatic edit
	// under --fix even though it's otherwise in fixer.FixableCodes; the
	// finding still fires and gets reported as usual. nil/true (the
	// default) means "fix if fixable."
	Fix *bool
	// Raw is every other key in this code's mapping, decoded as plain
	// YAML scalars/sequences — the rule-specific escape hatch.
	Raw map[string]any
}

// UnmarshalYAML decodes the two generic properties into their typed
// fields and keeps everything else, verbatim, in Raw — so a rule with a
// special property (e.g. "max_steps") doesn't need config to know its
// name or type in advance.
func (rc *RuleConfig) UnmarshalYAML(node *yaml.Node) error {
	var known struct {
		Severity *string `yaml:"severity"`
		Fix      *bool   `yaml:"fix"`
	}
	if err := node.Decode(&known); err != nil {
		return err
	}
	var raw map[string]any
	if err := node.Decode(&raw); err != nil {
		return err
	}
	delete(raw, "severity")
	delete(raw, "fix")
	rc.Severity = known.Severity
	rc.Fix = known.Fix
	rc.Raw = raw
	return nil
}

type Config struct {
	Ignores     []Ignore     `yaml:"ignore"`
	CustomRules []CustomRule `yaml:"custom_rules"`
	// Select narrows which rule codes/prefixes can fail "check". Empty
	// means every rule can gate the build (today's behavior). Independent
	// of Report.Select — see ReportConfig's doc comment.
	Select []string     `yaml:"select"`
	Report ReportConfig `yaml:"report"`
	// Rules is the rules: section (ADR 0002), keyed by exact rule code
	// — never a category prefix, unlike Select/Report.Select above.
	Rules map[string]RuleConfig `yaml:"rules"`
}

// RuleSeverity returns code's configured severity override ("blocker",
// "warning", or "info"), and whether one was set at all.
func (c *Config) RuleSeverity(code string) (string, bool) {
	rc, ok := c.Rules[code]
	if !ok || rc.Severity == nil {
		return "", false
	}
	return *rc.Severity, true
}

// RuleFixDisabled reports whether code's rules: entry sets fix: false.
// Exact-code lookup only — the rules: section deliberately doesn't
// support the category-prefix matching Select/Report.Select do (see
// ADR 0002's Migration section), so disabling every AZR* code's fix
// takes one entry per code now instead of one shared prefix.
func (c *Config) RuleFixDisabled(code string) bool {
	rc, ok := c.Rules[code]
	return ok && rc.Fix != nil && !*rc.Fix
}

// RuleRawInt reads an integer-valued special property (e.g.
// STRUCT002's max_steps, TIMEOUT001's fix_default) out of code's Raw
// map. ok is false if code has no rules: entry, the key is absent, or
// its value isn't a number — callers fall back to the rule's own
// default in every one of those cases identically.
func (c *Config) RuleRawInt(code, key string) (int, bool) {
	rc, ok := c.Rules[code]
	if !ok {
		return 0, false
	}
	switch v := rc.Raw[key].(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case uint64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

// RuleRawStringSlice reads a string-list special property (e.g.
// PERF001/LEAN010's cached_runners) out of code's Raw map. ok is false
// under the same absence/wrong-type conditions as RuleRawInt.
func (c *Config) RuleRawStringSlice(code, key string) ([]string, bool) {
	rc, ok := c.Rules[code]
	if !ok {
		return nil, false
	}
	raw, ok := rc.Raw[key].([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out, true
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
