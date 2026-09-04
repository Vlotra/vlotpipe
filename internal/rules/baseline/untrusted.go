package baseline

import (
	"regexp"
	"strings"
)

// exprPattern matches a GitHub Actions template expression, e.g.
// "${{ github.event.issue.title }}", capturing the inner expression text.
var exprPattern = regexp.MustCompile(`\$\{\{\s*([^}]+?)\s*\}\}`)

// untrustedContexts are expression paths an attacker can fully control by
// submitting a PR, issue, comment, or commit — the standard "untrusted
// input" list used by GitHub's own docs and static analyzers like zizmor
// and CodeQL for detecting script-injection risk.
var untrustedContexts = []string{
	"github.event.issue.title",
	"github.event.issue.body",
	"github.event.pull_request.title",
	"github.event.pull_request.body",
	"github.event.pull_request.head.ref",
	"github.event.pull_request.head.label",
	"github.event.pull_request.head.repo.full_name",
	"github.event.comment.body",
	"github.event.review.body",
	"github.event.review_comment.body",
	"github.event.discussion.title",
	"github.event.discussion.body",
	"github.event.head_commit.message",
	"github.event.commits",
	"github.event.pages",
	"github.head_ref",
}

// findUntrustedExprs returns every "${{ ... }}" expression in s whose inner
// path references attacker-controllable input.
func findUntrustedExprs(s string) []string {
	if s == "" {
		return nil
	}
	var found []string
	for _, m := range exprPattern.FindAllStringSubmatch(s, -1) {
		expr := m[1]
		for _, ctx := range untrustedContexts {
			if strings.Contains(expr, ctx) {
				found = append(found, m[0])
				break
			}
		}
	}
	return found
}
