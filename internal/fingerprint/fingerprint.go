// Package fingerprint detects near-duplicate jobs within a set of scanned
// pipelines — copy-pasted jobs repeated across workflow files with only
// minor edits (a different version pin, a different secret ref, a tweaked
// timeout). It normalizes each job's steps into a token stream and hashes
// that stream with simhash, so structurally-identical blocks cluster
// together even when they're not byte-identical. This is the free-tier,
// single-scan slice of the "Insights" direction: cross-repo clustering,
// drift diffing, and golden-template synthesis are dashboard-side (see
// docs/adr/0004-duplicate-job-fingerprinting.md).
package fingerprint

import (
	"math/bits"
	"regexp"
	"sort"
	"strings"

	"github.com/vlotra/vlotpipe/internal/model"
)

// Code identifies a duplicate-job-cluster finding for suppression
// purposes — the same role every rules.Violation's Code plays, even
// though a cluster isn't a rules.Violation (it can span multiple files,
// which a single Violation's one Path can't represent). Supports the
// same inline "# vlotpipe: ignore[DUP001]" comment on a job's key line
// every other rule already supports (checked in BuildChunks below, via
// model.Pipeline.IsSuppressed — no new parser needed), and the same
// .vlotpipe.yml ignore: entries (checked by the caller, in main.go,
// since path-based ignore: needs internal/config, which this package
// deliberately doesn't depend on — see ADR 0004's Consequences).
const Code = "DUP001"

// DefaultThreshold is the similarity (0-1) above which two job signatures
// are considered the same cluster. 0.90 rather than the 0.85 the source
// research suggested: a job with 4-5 steps flips several simhash bits from
// a single differing token (e.g. a bumped version pin), so 0.85 pulled in
// pairs that only shared a "checkout + install" prefix. Recalibrate once
// there's a real corpus to tune against (see ADR 0004's open questions).
const DefaultThreshold = 0.90

// minSteps is the fewest steps a job needs before it's considered for
// fingerprinting. Below this, two jobs that both happen to start with
// "actions/checkout" trivially "match" — noise, not a real duplicate.
const minSteps = 3

// Chunk is one job's fingerprint: enough to point back at its source and
// compare it against others, but never the job's actual content. This is
// deliberate — it's the shape that's safe to send to a hosted dashboard
// (see pushreport.Payload.Fingerprints) without leaking pipeline internals
// (secret names, deploy targets) off the scanning machine.
type Chunk struct {
	Path      string `json:"path"`
	JobID     string `json:"job_id"`
	JobName   string `json:"job_name,omitempty"`
	Line      int    `json:"line"`
	Signature uint64 `json:"signature"`
}

// exprPattern matches a GitHub Actions expression block, e.g.
// "${{ secrets.API_KEY }}" or "${{ github.ref }}" — normalized away before
// hashing so two steps that differ only in which secret/ref they reference
// still fingerprint as the same shape.
var exprPattern = regexp.MustCompile(`\$\{\{[^}]*\}\}`)

var whitespacePattern = regexp.MustCompile(`\s+`)

// BuildChunks fingerprints every job across pipelines that has at least
// minSteps steps.
func BuildChunks(pipelines []*model.Pipeline) []Chunk {
	var out []Chunk
	for _, p := range pipelines {
		for _, j := range p.Jobs {
			if len(j.Steps) < minSteps {
				continue
			}
			if p.IsSuppressed(j.Line, Code) {
				continue
			}
			text := normalizeJob(j)
			if text == "" {
				continue
			}
			out = append(out, Chunk{
				Path:      p.Path,
				JobID:     j.ID,
				JobName:   j.Name,
				Line:      j.Line,
				Signature: simhash(text),
			})
		}
	}
	return out
}

// normalizeJob reduces a job's steps to a token stream that's stable
// across the kind of cosmetic drift copy-pasting introduces: an action's
// version pin, or which secret/ref an expression references. A "uses"
// step contributes its action name only (the part before "@"); a "run"
// step contributes its script with expression blocks replaced by a single
// placeholder and whitespace collapsed.
func normalizeJob(j model.Job) string {
	var tokens []string
	for _, s := range j.Steps {
		switch {
		case s.Uses != "":
			name, _, _ := strings.Cut(s.Uses, "@")
			tokens = append(tokens, "uses:"+name)
		case s.Run != "":
			run := exprPattern.ReplaceAllString(s.Run, "<expr>")
			run = whitespacePattern.ReplaceAllString(strings.TrimSpace(run), " ")
			if run != "" {
				tokens = append(tokens, "run:"+run)
			}
		}
	}
	return strings.Join(tokens, "\n")
}

// simhash computes a 64-bit simhash of text's whitespace-delimited tokens,
// weighted by term frequency: for each token, hash it, then for each bit
// position add +1 to that bit's counter if the hash bit is 1, else -1; the
// final signature has bit i set wherever counter i ended up positive. Two
// texts that share most tokens end up with signatures that differ in only
// a few bits, which HammingDistance/Similarity below measure directly —
// unlike a cryptographic hash, where a one-token difference scrambles the
// entire output.
func simhash(text string) uint64 {
	tokens := strings.Fields(text)
	if len(tokens) == 0 {
		return 0
	}
	var counts [64]int
	for _, tok := range tokens {
		h := fnv1a64(tok)
		for i := 0; i < 64; i++ {
			if h&(1<<uint(i)) != 0 {
				counts[i]++
			} else {
				counts[i]--
			}
		}
	}
	var sig uint64
	for i := 0; i < 64; i++ {
		if counts[i] > 0 {
			sig |= 1 << uint(i)
		}
	}
	return sig
}

// fnv1a64 is the FNV-1a hash — used instead of hash/maphash because
// maphash is seeded per-process, and two Chunks compared across separate
// vlotpipe invocations (e.g. a future dashboard comparing fingerprints
// submitted by different CI runs) need the same token to always hash to
// the same value.
func fnv1a64(s string) uint64 {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}

// HammingDistance returns the number of differing bits between two
// 64-bit signatures.
func HammingDistance(a, b uint64) int {
	return bits.OnesCount64(a ^ b)
}

// Similarity converts a Hamming distance between two 64-bit signatures
// into a 0-1 score, where 1.0 means identical.
func Similarity(a, b uint64) float64 {
	return 1 - float64(HammingDistance(a, b))/64
}

// Group is a set of chunks whose signatures are all mutually similar
// enough (at or above the clustering threshold) to be flagged as
// duplicates of each other.
type Group struct {
	Members []Chunk
}

// Cluster groups chunks whose pairwise similarity is at or above
// threshold, using single-linkage union-find: chunk A joins chunk B's
// cluster if any existing member of B's cluster is similar enough to A.
// Chunks scanned across many files but with no near-duplicate anywhere
// else form no group at all (a group of one isn't reported). Deliberately
// O(n^2) — this runs once per local `vlotpipe scan` invocation over one
// repo's jobs, not across a fleet, so the input size that matters here is
// small; the fleet-wide, non-quadratic comparison is the dashboard's job
// (see ADR 0004).
func Cluster(chunks []Chunk, threshold float64) []Group {
	n := len(chunks)
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	union := func(i, j int) {
		ri, rj := find(i), find(j)
		if ri != rj {
			parent[ri] = rj
		}
	}

	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if Similarity(chunks[i].Signature, chunks[j].Signature) >= threshold {
				union(i, j)
			}
		}
	}

	byRoot := map[int][]Chunk{}
	for i, c := range chunks {
		root := find(i)
		byRoot[root] = append(byRoot[root], c)
	}

	var groups []Group
	for _, members := range byRoot {
		if len(members) < 2 {
			continue
		}
		groups = append(groups, Group{Members: members})
	}
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].Members) != len(groups[j].Members) {
			return len(groups[i].Members) > len(groups[j].Members)
		}
		return groups[i].Members[0].Path < groups[j].Members[0].Path
	})
	return groups
}
