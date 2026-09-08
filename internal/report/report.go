// Package report formats rule violations for humans (ruff-style compact
// text) and for machines (JSON, for CI integrations).
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/fatih/color"

	"github.com/vlotra/vlotpipe/internal/fingerprint"
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

// DuplicateClusters prints every near-duplicate job cluster found in this
// scan, listing each member's exact location — the same file:line
// precision every other finding in this report gets. That's deliberate:
// which jobs in *this* scan look like duplicates of each other is free,
// local, single-invocation signal, no different in kind from any other
// rule's output, so there's no reason to withhold it. What's actually
// paid (Insights) is aggregating this across every repo in an org over
// time — drift between "duplicate" copies, a golden-template suggestion
// — which no single local scan can do regardless of what this function
// prints. Callers should skip calling this at all when groups is empty:
// a "0 found" line is noise, not a teaser. See ADR 0004.
func DuplicateClusters(w io.Writer, groups []fingerprint.Group) {
	if len(groups) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "%d duplicate job cluster%s found:\n\n", len(groups), plural(len(groups)))
	for i, g := range groups {
		fmt.Fprintf(w, "  cluster %d (%d jobs, %.0f%% similar):\n", i+1, len(g.Members), minPairwiseSimilarity(g.Members)*100)
		for _, m := range g.Members {
			fmt.Fprintf(w, "    %s job %q\n", dimColor.Sprintf("%s:%d", m.Path, m.Line), jobLabel(m))
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "Local, single-scan duplicate detection is free — that's the list above."+
		" Insights (paid, coming soon) aggregates this across every repo in your org,"+
		" tracks drift between \"duplicate\" copies over time, and suggests a golden"+
		" template to collapse them into — none of which a single local scan can do.")
}

// jobLabel prefers a job's display name over its raw workflow-file ID,
// since Name (e.g. "Codeception Backend Tests") is what a human
// recognizes the job by; falls back to JobID when Name wasn't set.
func jobLabel(c fingerprint.Chunk) string {
	if c.JobName != "" {
		return c.JobName
	}
	return c.JobID
}

// minPairwiseSimilarity is the lowest similarity between any two members
// of a cluster — the conservative number to show, since it's the weakest
// link that actually justifies grouping them together at all.
func minPairwiseSimilarity(members []fingerprint.Chunk) float64 {
	min := 1.0
	for i := 0; i < len(members); i++ {
		for j := i + 1; j < len(members); j++ {
			if s := fingerprint.Similarity(members[i].Signature, members[j].Signature); s < min {
				min = s
			}
		}
	}
	return min
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
