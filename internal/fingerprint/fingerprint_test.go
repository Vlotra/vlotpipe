package fingerprint

import (
	"testing"

	"github.com/vlotra/vlotpipe/internal/model"
)

func jobWithSteps(steps ...model.Step) model.Job {
	return model.Job{ID: "build", Steps: steps}
}

func TestBuildChunksSkipsJobsBelowMinSteps(t *testing.T) {
	p := &model.Pipeline{
		Path: "a.yml",
		Jobs: []model.Job{
			jobWithSteps(
				model.Step{Uses: "actions/checkout@v4"},
				model.Step{Uses: "actions/setup-node@v4"},
			),
		},
	}
	chunks := BuildChunks([]*model.Pipeline{p})
	if len(chunks) != 0 {
		t.Fatalf("BuildChunks with a 2-step job = %d chunks, want 0 (below minSteps)", len(chunks))
	}
}

func TestBuildChunksIncludesJobsAtMinSteps(t *testing.T) {
	p := &model.Pipeline{
		Path: "a.yml",
		Jobs: []model.Job{
			jobWithSteps(
				model.Step{Uses: "actions/checkout@v4"},
				model.Step{Uses: "actions/setup-node@v4"},
				model.Step{Run: "npm test"},
			),
		},
	}
	chunks := BuildChunks([]*model.Pipeline{p})
	if len(chunks) != 1 {
		t.Fatalf("BuildChunks with a 3-step job = %d chunks, want 1", len(chunks))
	}
}

func identicalTestJob() model.Job {
	return jobWithSteps(
		model.Step{Uses: "actions/checkout@v4"},
		model.Step{Uses: "actions/setup-node@v20"},
		model.Step{Run: "npm ci"},
		model.Step{Run: "npm test"},
	)
}

func TestIdenticalJobsProduceIdenticalSignatures(t *testing.T) {
	a := simhash(normalizeJob(identicalTestJob()))
	b := simhash(normalizeJob(identicalTestJob()))
	if a != b {
		t.Fatalf("identical jobs produced different signatures: %d vs %d", a, b)
	}
	if Similarity(a, b) != 1.0 {
		t.Fatalf("Similarity(identical, identical) = %v, want 1.0", Similarity(a, b))
	}
}

// A copy-pasted job that only bumped a version pin and swapped which
// secret an expression references is exactly the "duplicate with drift"
// case the feature exists to catch — it must still cluster.
func TestJobsDifferingOnlyByVersionPinAndSecretRefAreNearDuplicates(t *testing.T) {
	original := jobWithSteps(
		model.Step{Uses: "actions/checkout@v3"},
		model.Step{Uses: "actions/setup-node@v18"},
		model.Step{Run: "echo ${{ secrets.NPM_TOKEN }} > .npmrc"},
		model.Step{Run: "npm ci"},
		model.Step{Run: "npm test"},
	)
	drifted := jobWithSteps(
		model.Step{Uses: "actions/checkout@v4"},
		model.Step{Uses: "actions/setup-node@v20"},
		model.Step{Run: "echo ${{ secrets.NPM_TOKEN_STAGING }} > .npmrc"},
		model.Step{Run: "npm ci"},
		model.Step{Run: "npm test"},
	)
	sig1 := simhash(normalizeJob(original))
	sig2 := simhash(normalizeJob(drifted))
	if sim := Similarity(sig1, sig2); sim < DefaultThreshold {
		t.Fatalf("Similarity(original, drifted) = %v, want >= %v (version pin + secret ref drift should still cluster)", sim, DefaultThreshold)
	}
}

func TestStructurallyDifferentJobsAreNotNearDuplicates(t *testing.T) {
	a := jobWithSteps(
		model.Step{Uses: "actions/checkout@v4"},
		model.Step{Uses: "actions/setup-node@v20"},
		model.Step{Run: "npm ci"},
		model.Step{Run: "npm test"},
	)
	b := jobWithSteps(
		model.Step{Uses: "actions/checkout@v4"},
		model.Step{Uses: "docker/build-push-action@v5"},
		model.Step{Run: "docker build -t app ."},
		model.Step{Run: "docker push app"},
	)
	sig1 := simhash(normalizeJob(a))
	sig2 := simhash(normalizeJob(b))
	if sim := Similarity(sig1, sig2); sim >= DefaultThreshold {
		t.Fatalf("Similarity(node job, docker job) = %v, want < %v", sim, DefaultThreshold)
	}
}

func TestClusterGroupsNearDuplicatesAcrossFiles(t *testing.T) {
	pA := &model.Pipeline{Path: "repo-a/deploy.yml", Jobs: []model.Job{identicalTestJob()}}
	pB := &model.Pipeline{Path: "repo-b/release.yml", Jobs: []model.Job{identicalTestJob()}}
	pC := &model.Pipeline{Path: "repo-c/docker.yml", Jobs: []model.Job{
		jobWithSteps(
			model.Step{Uses: "docker/build-push-action@v5"},
			model.Step{Run: "docker build -t app ."},
			model.Step{Run: "docker push app"},
		),
	}}

	chunks := BuildChunks([]*model.Pipeline{pA, pB, pC})
	if len(chunks) != 3 {
		t.Fatalf("BuildChunks = %d chunks, want 3", len(chunks))
	}

	groups := Cluster(chunks, DefaultThreshold)
	if len(groups) != 1 {
		t.Fatalf("Cluster = %d groups, want 1 (the two identical jobs)", len(groups))
	}
	if len(groups[0].Members) != 2 {
		t.Fatalf("cluster has %d members, want 2", len(groups[0].Members))
	}
	paths := map[string]bool{groups[0].Members[0].Path: true, groups[0].Members[1].Path: true}
	if !paths["repo-a/deploy.yml"] || !paths["repo-b/release.yml"] {
		t.Fatalf("cluster members = %+v, want repo-a/deploy.yml and repo-b/release.yml", groups[0].Members)
	}
}

func TestClusterOmitsSingletonGroups(t *testing.T) {
	chunks := []Chunk{{Path: "a.yml", JobID: "only", Signature: 0x1}}
	groups := Cluster(chunks, DefaultThreshold)
	if len(groups) != 0 {
		t.Fatalf("Cluster with one chunk = %d groups, want 0 (no duplicate to report)", len(groups))
	}
}

func TestHammingDistanceAndSimilarity(t *testing.T) {
	if d := HammingDistance(0b1010, 0b1010); d != 0 {
		t.Errorf("HammingDistance(x, x) = %d, want 0", d)
	}
	if d := HammingDistance(0, ^uint64(0)); d != 64 {
		t.Errorf("HammingDistance(0, all-ones) = %d, want 64", d)
	}
	if s := Similarity(0, ^uint64(0)); s != 0 {
		t.Errorf("Similarity(0, all-ones) = %v, want 0", s)
	}
}
