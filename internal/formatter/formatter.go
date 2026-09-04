// Package formatter reformats pipeline YAML to a single consistent
// style, the way gofmt does for Go: strict, total normalization rather
// than a minimal diff. It parses the file, reorders each mapping's keys
// into a canonical, well-known order (workflow/pipeline root, then job,
// then step — the order GitHub's and Azure's own documentation present
// keys in, not alphabetical), and re-encodes with a fixed 2-space indent
// (the convention already used throughout both platforms' own docs and
// examples).
//
// This is a genuinely different trade-off than internal/fixer. fixer
// edits raw text directly so it touches nothing but the lines it's
// adding — the point there is a diff a reviewer can read in five
// seconds. Format goes through a full parse/reorder/re-encode pass
// instead, which normalizes indentation and key order throughout the
// whole file but, as a direct consequence, does not preserve blank
// lines between blocks (yaml.v3's node model doesn't track them at all)
// or some stylistic choices like list-item indent width or quote style
// in edge cases. Line/head/foot comments do survive the round-trip and
// stay attached to their key as it moves. The first run against a
// hand-formatted file is expected to produce a large diff — that's
// gofmt's own trade-off too, not a bug.
package formatter

import (
	"bytes"
	"sort"

	"gopkg.in/yaml.v3"

	"github.com/vlotra/vlotpipe/internal/model"
	azureparser "github.com/vlotra/vlotpipe/internal/parser/azure"
)

const indent = 2

// Format re-serializes raw YAML with canonical key order and a
// consistent indent. Content that fails to parse, or that has no
// document content at all (an empty file), is returned unchanged rather
// than erroring — an unparseable file is the scanner's problem to
// report, not this package's.
func Format(platform model.Platform, raw []byte) ([]byte, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(raw, &root); err != nil {
		return nil, err
	}
	if len(root.Content) == 0 {
		return raw, nil
	}
	doc := root.Content[0]
	if doc.Kind == yaml.MappingNode {
		switch platform {
		case model.PlatformGitHubActions:
			reorderGitHub(doc)
		case model.PlatformAzurePipelines:
			reorderAzure(doc)
		}
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(indent)
	if err := enc.Encode(&root); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// keyOrder returns a rank for each key name, lowest first; a key not
// listed sorts after every listed one, keeping its position relative to
// other unlisted keys (a stable sort, so an unrecognized/custom key
// never gets shuffled relative to its unrecognized neighbors).
func keyOrder(names ...string) map[string]int {
	out := make(map[string]int, len(names))
	for i, n := range names {
		out[n] = i
	}
	return out
}

var ghWorkflowOrder = keyOrder(
	"name", "on", "permissions", "env", "defaults", "concurrency", "jobs",
)

var ghJobOrder = keyOrder(
	"name", "needs", "if", "runs-on", "permissions", "environment",
	"concurrency", "outputs", "env", "defaults", "timeout-minutes",
	"strategy", "continue-on-error", "container", "services",
	"uses", "with", "secrets", "steps",
)

var ghStepOrder = keyOrder(
	"id", "if", "name", "uses", "run", "shell", "with", "env",
	"continue-on-error", "timeout-minutes", "working-directory",
)

var azureRootOrder = keyOrder(
	"name", "trigger", "pr", "resources", "pool", "variables",
	"parameters", "extends", "stages", "jobs", "steps",
)

var azureStageOrder = keyOrder(
	"stage", "displayName", "dependsOn", "condition", "pool", "variables", "jobs",
)

var azureJobOrder = keyOrder(
	"job", "displayName", "dependsOn", "condition", "pool",
	"timeoutInMinutes", "cancelTimeoutInMinutes", "strategy",
	"continueOnError", "variables", "workspace", "container",
	"services", "steps",
)

// azureStepKindKeys identifies which of a step's keys names its "kind"
// — exactly one is present per step — so it can be ranked first,
// regardless of which one it is.
var azureStepOrder = keyOrder(
	"task", "script", "bash", "powershell", "pwsh", "checkout",
	"publish", "download", "template",
	"displayName", "name", "condition", "continueOnError",
	"timeoutInMinutes", "inputs", "parameters", "env",
)

// reorderMapping stable-sorts n's key/value pairs by each key's rank in
// order, leaving unranked keys after every ranked one, in their
// original relative order.
func reorderMapping(n *yaml.Node, order map[string]int) {
	if n == nil || n.Kind != yaml.MappingNode {
		return
	}
	type pair struct{ key, value *yaml.Node }
	pairs := make([]pair, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		pairs = append(pairs, pair{n.Content[i], n.Content[i+1]})
	}
	rank := func(key string) int {
		if r, ok := order[key]; ok {
			return r
		}
		return len(order)
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		return rank(pairs[i].key.Value) < rank(pairs[j].key.Value)
	})
	content := make([]*yaml.Node, 0, len(n.Content))
	for _, p := range pairs {
		content = append(content, p.key, p.value)
	}
	n.Content = content
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func reorderGitHub(doc *yaml.Node) {
	reorderMapping(doc, ghWorkflowOrder)
	jobsNode := mapValue(doc, "jobs")
	if jobsNode == nil || jobsNode.Kind != yaml.MappingNode {
		return
	}
	for i := 1; i < len(jobsNode.Content); i += 2 {
		jobNode := jobsNode.Content[i]
		reorderMapping(jobNode, ghJobOrder)
		if stepsNode := mapValue(jobNode, "steps"); stepsNode != nil && stepsNode.Kind == yaml.SequenceNode {
			for _, stepNode := range stepsNode.Content {
				reorderMapping(stepNode, ghStepOrder)
			}
		}
	}
}

func reorderAzure(doc *yaml.Node) {
	reorderMapping(doc, azureRootOrder)
	if stagesNode := mapValue(doc, "stages"); stagesNode != nil && stagesNode.Kind == yaml.SequenceNode {
		for _, stageNode := range azureparser.FlattenTemplateExpressions(stagesNode) {
			// Mirrors parseStages: a stage with a "template:" key is an
			// opaque include, never treated as a real stage.
			if stageNode.Kind != yaml.MappingNode || mapValue(stageNode, "template") != nil {
				continue
			}
			reorderMapping(stageNode, azureStageOrder)
			if jobsNode := mapValue(stageNode, "jobs"); jobsNode != nil && jobsNode.Kind == yaml.SequenceNode {
				reorderAzureJobs(jobsNode)
			}
		}
	}
	if jobsNode := mapValue(doc, "jobs"); jobsNode != nil && jobsNode.Kind == yaml.SequenceNode {
		reorderAzureJobs(jobsNode)
	}
	if stepsNode := mapValue(doc, "steps"); stepsNode != nil && stepsNode.Kind == yaml.SequenceNode {
		reorderAzureSteps(stepsNode)
	}
}

func reorderAzureJobs(jobsNode *yaml.Node) {
	for _, jobNode := range azureparser.FlattenTemplateExpressions(jobsNode) {
		// A job with a "template:" key is an opaque include (parseJobs
		// itself skips these entirely, never treating one as a real
		// job) — its keys (template, parameters) aren't a job's own
		// shape, so running it through azureJobOrder would misfile
		// "parameters" as an unranked trailing key of a job that isn't
		// really there.
		if jobNode.Kind != yaml.MappingNode || mapValue(jobNode, "template") != nil {
			continue
		}
		reorderMapping(jobNode, azureJobOrder)
		if stepsNode := mapValue(jobNode, "steps"); stepsNode != nil && stepsNode.Kind == yaml.SequenceNode {
			reorderAzureSteps(stepsNode)
		}
	}
}

func reorderAzureSteps(stepsNode *yaml.Node) {
	for _, stepNode := range azureparser.FlattenTemplateExpressions(stepsNode) {
		if stepNode.Kind != yaml.MappingNode {
			continue
		}
		// Unlike a template stage/job, a template *step* is a real,
		// recognized step kind (azure.go's parseSteps handles it
		// alongside task/script/checkout) — azureStepOrder already
		// ranks "template" as an identifying key, so it goes through
		// reordering normally rather than being skipped as opaque.
		reorderMapping(stepNode, azureStepOrder)
	}
}
