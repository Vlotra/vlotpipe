package pushreport

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/vlotra/vlotpipe/internal/rules"
)

func TestPushSendsExpectedPayloadAndHeaders(t *testing.T) {
	var gotAuth, gotContentType string
	var gotBody Payload

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &gotBody); err != nil {
			t.Errorf("server: bad JSON body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	payload := Payload{
		Repo:         "example/repo",
		Branch:       "main",
		Commit:       "abc123",
		ScannedAt:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		FilesScanned: 3,
		Violations: []rules.Violation{
			{Code: "SEC001", Severity: rules.SeverityBlocker, Message: "m", Path: "a.yml", Line: 1, Col: 1},
		},
	}

	if err := Push(srv.URL, "secret-token", payload); err != nil {
		t.Fatalf("Push: %v", err)
	}

	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization header = %q, want %q", gotAuth, "Bearer secret-token")
	}
	if gotContentType != "application/json" {
		t.Errorf("Content-Type header = %q, want application/json", gotContentType)
	}
	if gotBody.Repo != "example/repo" || gotBody.Branch != "main" || gotBody.Commit != "abc123" {
		t.Errorf("payload identity fields = %+v, want repo/branch/commit preserved", gotBody)
	}
	if len(gotBody.Violations) != 1 || gotBody.Violations[0].Code != "SEC001" {
		t.Errorf("payload violations = %+v, want the one SEC001 finding", gotBody.Violations)
	}
}

func TestPushOmitsAuthHeaderWhenTokenEmpty(t *testing.T) {
	var gotAuth string
	sawRequest := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawRequest = true
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if err := Push(srv.URL, "", Payload{}); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if !sawRequest {
		t.Fatal("server never received a request")
	}
	if gotAuth != "" {
		t.Errorf("Authorization header = %q, want empty (no token given)", gotAuth)
	}
}

func TestPushReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("invalid token"))
	}))
	defer srv.Close()

	err := Push(srv.URL, "bad-token", Payload{})
	if err == nil {
		t.Fatal("expected an error for a 401 response, got nil")
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("error = %q, want it to include the response body for debuggability", err.Error())
	}
}

func TestPushReturnsErrorOnUnreachableURL(t *testing.T) {
	err := Push("http://127.0.0.1:1/does-not-exist", "", Payload{})
	if err == nil {
		t.Fatal("expected an error for an unreachable address, got nil")
	}
}

func TestGitMetadataOnNonGitDirectoryReturnsEmptyStrings(t *testing.T) {
	dir := t.TempDir()
	repo, branch, commit := GitMetadata(dir)
	if repo != "" || branch != "" || commit != "" {
		t.Errorf("GitMetadata on a non-git directory = (%q, %q, %q), want all empty", repo, branch, commit)
	}
}

func TestGitMetadataOnRealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")
	run("commit", "--allow-empty", "-q", "-m", "init")

	_, branch, commit := GitMetadata(dir)
	if branch == "" {
		t.Error("expected a non-empty branch name from a real git repo")
	}
	if commit == "" {
		t.Error("expected a non-empty commit hash from a real git repo")
	}
}
