// Package pushreport sends a scan's complete, unfiltered finding set to
// a dashboard endpoint after a scan — the CLI-side half of vlotpipe's
// fleet-wide dashboard direction. The server itself is a separate,
// later project; this only defines the client contract: a JSON POST
// with every finding vlotpipe produced, never narrowed by --select or
// --report-select, since the entire point of centralizing findings
// across a team's repos is complete visibility even when a given repo's
// local gate or local output is scoped down.
package pushreport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/vlotra/vlotpipe/internal/fingerprint"
	"github.com/vlotra/vlotpipe/internal/rules"
)

// Payload is the full body sent to a report-to endpoint.
type Payload struct {
	Repo         string            `json:"repo"`
	Branch       string            `json:"branch"`
	Commit       string            `json:"commit"`
	ScannedAt    time.Time         `json:"scanned_at"`
	FilesScanned int               `json:"files_scanned"`
	Violations   []rules.Violation `json:"violations"`
	// Fingerprints is one entry per job with enough steps to fingerprint
	// (see internal/fingerprint), always the complete set regardless of
	// select/report-select — same "never narrowed" rule as Violations,
	// since cross-repo clustering needs every job, not just the ones a
	// given repo's local config chose to display. Each entry is a
	// structural signature plus a file/job pointer, never step content —
	// the dashboard can say "this job matches one in another repo" and
	// point at both locations without either raw YAML ever leaving the
	// scanning machine. See docs/adr/0004-duplicate-job-fingerprinting.md.
	Fingerprints []fingerprint.Chunk `json:"fingerprints,omitempty"`
}

const requestTimeout = 10 * time.Second

// Push JSON-encodes payload and POSTs it to url. If token is non-empty
// it's sent as an "Authorization: Bearer <token>" header; if empty, no
// Authorization header is sent at all (some dashboard endpoints — e.g.
// on a private network — may not require auth). Any non-2xx response is
// returned as an error with the response body included, for
// debuggability against whatever the actual dashboard contract turns
// out to need.
func Push(url, token string, payload Payload) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encoding payload: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	client := &http.Client{Timeout: requestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s responded %s: %s", url, resp.Status, strings.TrimSpace(string(respBody)))
	}
	return nil
}

// GitMetadata best-effort-derives repo/branch/commit identity from the
// git repository rooted at (or above) dir, for populating Payload.
// Every failure mode — dir isn't a git repo, no "origin" remote, no git
// binary on PATH — yields an empty string for that field rather than an
// error: this metadata is a nice-to-have for the dashboard to display,
// never something that should block a scan from completing.
func GitMetadata(dir string) (repo, branch, commit string) {
	repo = gitOutput(dir, "remote", "get-url", "origin")
	branch = gitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	commit = gitOutput(dir, "rev-parse", "HEAD")
	return repo, branch, commit
}

func gitOutput(dir string, args ...string) string {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
