// Package report formats rule violations for humans (ruff-style compact
// text) and for machines (JSON, for CI integrations).
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"

	"github.com/vlotra/vlotpipe/internal/rules"
)

var (
	blockerColor = color.New(color.FgRed, color.Bold)
	warningColor = color.New(color.FgYellow, color.Bold)
	infoColor    = color.New(color.FgCyan, color.Bold)
	dimColor     = color.New(color.Faint)
)

func severityColor(s rules.Severity) *color.Color {
	switch s {
	case rules.SeverityBlocker:
		return blockerColor
	case rules.SeverityWarning:
		return warningColor
	default:
		return infoColor
	}
}

// Text writes one line per violation in "path:line:col: CODE message" form,
// followed by a summary line, mirroring ruff/eslint-compact conventions.
func Text(w io.Writer, violations []rules.Violation, filesScanned int) {
	var blockers, warnings, infos int
	for _, v := range violations {
		loc := fmt.Sprintf("%s:%d:%d:", v.Path, v.Line, v.Col)
		fmt.Fprintf(w, "%s %s %s\n", dimColor.Sprint(loc), severityColor(v.Severity).Sprint(v.Code), v.Message)
		switch v.Severity {
		case rules.SeverityBlocker:
			blockers++
		case rules.SeverityWarning:
			warnings++
		default:
			infos++
		}
	}

	if len(violations) == 0 {
		fmt.Fprintf(w, "%s (%d file%s scanned)\n", color.New(color.FgGreen, color.Bold).Sprint("All checks passed!"), filesScanned, plural(filesScanned))
		return
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "Found %d violation%s across %d file%s (%d blocker%s, %d warning%s, %d info%s)\n",
		len(violations), plural(len(violations)),
		filesScanned, plural(filesScanned),
		blockers, plural(blockers),
		warnings, plural(warnings),
		infos, plural(infos),
	)
}

// DuplicateSummary prints a one-line teaser naming how many near-duplicate
// job clusters were found in this scan, without any detail about which
// jobs or where — the detail (repo spread, drift, a golden-template
// suggestion) is the paid Insights dashboard's job, not the free CLI's.
// Callers should skip calling this at all when clusters is 0: a "0 found"
// line is noise, not a teaser.
func DuplicateSummary(w io.Writer, clusters int) {
	fmt.Fprintf(w, "\n%d duplicate job cluster%s found across scanned files — full cross-repo breakdown + golden-template suggestions in Insights (paid, coming soon).\n",
		clusters, plural(clusters))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// JSON writes violations as a JSON array.
func JSON(w io.Writer, violations []rules.Violation) error {
	if violations == nil {
		violations = []rules.Violation{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(violations)
}

// GitHub writes violations as GitHub Actions workflow commands
// (https://docs.github.com/actions/using-workflows/workflow-commands-for-github-actions),
// which GitHub renders as inline annotations directly on the "Files
// changed" tab of a pull request — no bot, no app installation, no
// extra infrastructure, just this output format instead of "text" in
// whatever step already runs "vlotpipe check". Severity maps to
// GitHub's three annotation levels: blocker -> error, warning ->
// warning, info -> notice.
func GitHub(w io.Writer, violations []rules.Violation) {
	for _, v := range violations {
		level := "notice"
		switch v.Severity {
		case rules.SeverityBlocker:
			level = "error"
		case rules.SeverityWarning:
			level = "warning"
		}
		fmt.Fprintf(w, "::%s file=%s,line=%d,col=%d,title=%s::%s\n",
			level, v.Path, v.Line, v.Col, v.Code, escapeWorkflowCommand(v.Message))
	}
}

// AzureDevOps writes violations as Azure Pipelines logging commands
// (https://learn.microsoft.com/azure/devops/pipelines/scripts/logging-commands),
// which Azure DevOps renders as annotations on the build summary and,
// for a PR build, on the "Files" tab of the pull request. Azure's
// logging commands only define two issue types — error and warning —
// so info-severity violations map to warning, the less severe of the
// two rather than being silently dropped.
func AzureDevOps(w io.Writer, violations []rules.Violation) {
	for _, v := range violations {
		issueType := "warning"
		if v.Severity == rules.SeverityBlocker {
			issueType = "error"
		}
		fmt.Fprintf(w, "##vso[task.logissue type=%s;sourcepath=%s;linenumber=%d;columnnumber=%d]%s: %s\n",
			issueType, v.Path, v.Line, v.Col, v.Code, escapeWorkflowCommand(v.Message))
	}
}

// escapeWorkflowCommand applies the escaping both GitHub's and Azure's
// workflow/logging command formats require for free-text message
// content, so a violation message containing "%", a newline, or a
// carriage return can't break the command's own parsing.
func escapeWorkflowCommand(s string) string {
	s = strings.ReplaceAll(s, "%", "%25")
	s = strings.ReplaceAll(s, "\r", "%0D")
	s = strings.ReplaceAll(s, "\n", "%0A")
	return s
}
