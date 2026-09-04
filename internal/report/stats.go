package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/fatih/color"

	"github.com/vlotra/vlotpipe/internal/rules"
)

// Stats is an aggregate view over a scan's violations: which rules fire
// most, and which files need attention first. Built for scanning many
// repos/files at once, where a flat violation list stops being
// skimmable.
type Stats struct {
	FilesScanned    int            `json:"files_scanned"`
	TotalViolations int            `json:"total_violations"`
	BySeverity      map[string]int `json:"by_severity"`
	ByCode          []CodeCount    `json:"by_code"`
	ByFile          []FileCount    `json:"by_file"`
}

type CodeCount struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Count    int    `json:"count"`
}

type FileCount struct {
	Path     string `json:"path"`
	Blockers int    `json:"blockers"`
	Warnings int    `json:"warnings"`
	Infos    int    `json:"infos"`
	Total    int    `json:"total"`
}

// BuildStats aggregates a flat violation list into Stats.
func BuildStats(violations []rules.Violation, filesScanned int) Stats {
	s := Stats{
		FilesScanned:    filesScanned,
		TotalViolations: len(violations),
		BySeverity:      map[string]int{"blocker": 0, "warning": 0, "info": 0},
	}

	codeCounts := map[string]*CodeCount{}
	fileCounts := map[string]*FileCount{}

	for _, v := range violations {
		s.BySeverity[string(v.Severity)]++

		if codeCounts[v.Code] == nil {
			codeCounts[v.Code] = &CodeCount{Code: v.Code, Severity: string(v.Severity)}
		}
		codeCounts[v.Code].Count++

		if fileCounts[v.Path] == nil {
			fileCounts[v.Path] = &FileCount{Path: v.Path}
		}
		fc := fileCounts[v.Path]
		fc.Total++
		switch v.Severity {
		case rules.SeverityBlocker:
			fc.Blockers++
		case rules.SeverityWarning:
			fc.Warnings++
		default:
			fc.Infos++
		}
	}

	for _, c := range codeCounts {
		s.ByCode = append(s.ByCode, *c)
	}
	sort.Slice(s.ByCode, func(i, j int) bool {
		if s.ByCode[i].Count != s.ByCode[j].Count {
			return s.ByCode[i].Count > s.ByCode[j].Count
		}
		return s.ByCode[i].Code < s.ByCode[j].Code
	})

	for _, f := range fileCounts {
		s.ByFile = append(s.ByFile, *f)
	}
	sort.Slice(s.ByFile, func(i, j int) bool {
		a, b := s.ByFile[i], s.ByFile[j]
		if a.Blockers != b.Blockers {
			return a.Blockers > b.Blockers
		}
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Path < b.Path
	})

	return s
}

// maxFilesInText caps how many rows the text renderer prints in the
// "by file" table before summarizing the rest, so a scan across hundreds
// of files stays skimmable. JSON output is never truncated.
const maxFilesInText = 20

// StatsText renders Stats as three ruff-style tables: by severity, by
// rule code, and by file (worst offenders first).
func StatsText(w io.Writer, s Stats) {
	if s.TotalViolations == 0 {
		fmt.Fprintf(w, "%s (%d file%s scanned)\n", color.New(color.FgGreen, color.Bold).Sprint("All checks passed!"), s.FilesScanned, plural(s.FilesScanned))
		return
	}

	fmt.Fprintf(w, "%d file%s scanned, %d violation%s found\n\n",
		s.FilesScanned, plural(s.FilesScanned), s.TotalViolations, plural(s.TotalViolations))

	fmt.Fprintln(w, dimColor.Sprint("By severity"))
	for _, sev := range []string{"blocker", "warning", "info"} {
		n := s.BySeverity[sev]
		fmt.Fprintf(w, "  %s %d\n", severityColor(rules.Severity(sev)).Sprintf("%-9s", sev), n)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, dimColor.Sprint("By rule"))
	for _, c := range s.ByCode {
		fmt.Fprintf(w, "  %s %-11s %d\n", severityColor(rules.Severity(c.Severity)).Sprint("●"), c.Code, c.Count)
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, dimColor.Sprint("By file (worst first)"))
	shown := s.ByFile
	truncated := 0
	if len(shown) > maxFilesInText {
		truncated = len(shown) - maxFilesInText
		shown = shown[:maxFilesInText]
	}
	for _, f := range shown {
		fmt.Fprintf(w, "  %s  blockers=%d warnings=%d info=%d\n", f.Path, f.Blockers, f.Warnings, f.Infos)
	}
	if truncated > 0 {
		fmt.Fprintf(w, "  %s\n", dimColor.Sprintf("... and %d more file%s", truncated, plural(truncated)))
	}
}

// StatsJSON writes Stats as a single JSON object.
func StatsJSON(w io.Writer, s Stats) error {
	if s.ByCode == nil {
		s.ByCode = []CodeCount{}
	}
	if s.ByFile == nil {
		s.ByFile = []FileCount{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
