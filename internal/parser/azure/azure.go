// Package azure parses Azure Pipelines YAML files into the normalized
// internal/model representation — the same Pipeline/Job/Step shape the
// GitHub Actions parser produces, so platform-neutral rules (STRUCT*,
// most of LEAN*) work against either without change.
//
// Scope: a single-file pipeline definition. Azure's "template:" includes
// (at step, job, or stage level) are recorded as an opaque reference —
// the referenced file's contents are not fetched and inlined. That
// mirrors how the GitHub parser treats a reusable-workflow "uses:"
// call, and keeps this parser operating on one file at a time like its
// GitHub counterpart.
package azure

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/vlotra/vlotpipe/internal/model"
	"github.com/vlotra/vlotpipe/internal/parser/suppress"
)

// Detect reports whether path looks like an Azure Pipelines definition
// file. Unlike GitHub Actions (always under .github/workflows/), Azure
// has no fixed folder convention — the file is commonly named
// azure-pipelines.yml at the repo root, but the name is configurable in
// the Azure DevOps UI. This matches the conventional name at any depth.
func Detect(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "azure-pipelines.yml", "azure-pipelines.yaml", ".azure-pipelines.yml", ".azure-pipelines.yaml":
		return true
	default:
		return false
	}
}

// ParseFile reads and parses a single Azure Pipelines file.
func ParseFile(path string) (*model.Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, data)
}

// Parse parses Azure Pipelines YAML into a model.Pipeline.
func Parse(path string, data []byte) (*model.Pipeline, error) {
	p := &model.Pipeline{Platform: model.PlatformAzurePipelines, Path: path}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	p.Suppressions = suppress.Parse(&root)
	if len(root.Content) == 0 {
		return p, nil
	}
	doc := root.Content[0]
	if doc.Kind != yaml.MappingNode {
		return p, nil
	}

	if n := mapValue(doc, "name"); n != nil {
		p.Name = n.Value
	}
	if n := mapValue(doc, "trigger"); n != nil && !isNoneTrigger(n) {
		p.OnEvents = append(p.OnEvents, "push")
	}
	if n := mapValue(doc, "pr"); n != nil && !isNoneTrigger(n) {
		p.OnEvents = append(p.OnEvents, "pull_request")
	}

	rootPool := mapValue(doc, "pool")

	switch {
	case mapValue(doc, "stages") != nil:
		parseStages(mapValue(doc, "stages"), rootPool, p)
	case mapValue(doc, "jobs") != nil:
		parseJobs(mapValue(doc, "jobs"), "", rootPool, p)
	case mapValue(doc, "steps") != nil:
		// No jobs/stages block: an implicit single job wrapping the
		// top-level steps, matching how Azure itself runs a bare
		// "steps:" pipeline as one job on the root pool.
		p.Jobs = append(p.Jobs, model.Job{
			ID:     "(top-level steps)",
			Name:   "(top-level steps)",
			RunsOn: poolLabel(rootPool),
			Line:   doc.Line,
			Col:    doc.Column,
			Steps:  parseSteps(mapValue(doc, "steps")),
		})
	}

	return p, nil
}

// FindJobNode re-walks a raw Azure Pipelines document (stages/jobs, with
// the same "${{ if }}"/"${{ each }}" flattening Parse itself applies) and
// returns the job mapping node whose position matches line/col — the
// same (Line, Col) already reported on model.Job.Line/Col for that job.
// Exported so internal/fixer can locate a job's own YAML node to insert
// a new key next to (e.g. AZR001's fix) without re-implementing this
// traversal, which needs to stay in exact sync with Parse's.
func FindJobNode(doc *yaml.Node, line, col int) *yaml.Node {
	if doc == nil || doc.Kind != yaml.MappingNode {
		return nil
	}
	var found *yaml.Node
	visitJob := func(jobNode *yaml.Node) {
		if jobNode.Kind == yaml.MappingNode && jobNode.Line == line && jobNode.Column == col {
			found = jobNode
		}
	}
	if stagesNode := mapValue(doc, "stages"); stagesNode != nil && stagesNode.Kind == yaml.SequenceNode {
		for _, stageNode := range FlattenTemplateExpressions(stagesNode) {
			if stageNode.Kind != yaml.MappingNode || mapValue(stageNode, "template") != nil {
				continue
			}
			if jobsNode := mapValue(stageNode, "jobs"); jobsNode != nil && jobsNode.Kind == yaml.SequenceNode {
				for _, jobNode := range FlattenTemplateExpressions(jobsNode) {
					visitJob(jobNode)
				}
			}
		}
	}
	if jobsNode := mapValue(doc, "jobs"); jobsNode != nil && jobsNode.Kind == yaml.SequenceNode {
		for _, jobNode := range FlattenTemplateExpressions(jobsNode) {
			visitJob(jobNode)
		}
	}
	return found
}

func isNoneTrigger(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Value == "none"
}

// poolLabel extracts a runner label from a "pool:" node — vmImage for a
// Microsoft-hosted pool, or the pool name for a self-hosted one. This is
// the closest Azure equivalent of GitHub's "runs-on:".
func poolLabel(n *yaml.Node) string {
	if n == nil {
		return ""
	}
	if n.Kind == yaml.ScalarNode {
		return n.Value
	}
	if n.Kind != yaml.MappingNode {
		return ""
	}
	if v := mapValue(n, "vmImage"); v != nil {
		return v.Value
	}
	if v := mapValue(n, "name"); v != nil {
		return v.Value
	}
	return ""
}

// FlattenTemplateExpressions expands Azure's compile-time "${{ if ... }}:"
// and "${{ each x in y }}:" conditional-insertion syntax, which wraps a
// nested sequence of items (jobs, stages, or steps — this syntax is
// valid in any of them) under a single-key mapping whose key starts with
// "${{". Without this, that wrapper node gets parsed as if it were
// itself a job/stage/step with no real content: an empty ID, no steps,
// nothing — which is worse than useless, it's a misleading finding.
// Static analysis can't evaluate the condition, so this always expands
// the nested items as unconditionally present, which is the right
// default for linting: flag what's there, not what might run.
func FlattenTemplateExpressions(seq *yaml.Node) []*yaml.Node {
	var out []*yaml.Node
	for _, item := range seq.Content {
		if item.Kind == yaml.MappingNode && len(item.Content) == 2 {
			key := item.Content[0]
			value := item.Content[1]
			if key.Kind == yaml.ScalarNode && strings.HasPrefix(strings.TrimSpace(key.Value), "${{") && value.Kind == yaml.SequenceNode {
				out = append(out, FlattenTemplateExpressions(value)...)
				continue
			}
		}
		out = append(out, item)
	}
	return out
}

func parseStages(stagesNode *yaml.Node, rootPool *yaml.Node, p *model.Pipeline) {
	if stagesNode.Kind != yaml.SequenceNode {
		return
	}
	for _, stageNode := range FlattenTemplateExpressions(stagesNode) {
		if stageNode.Kind != yaml.MappingNode {
			continue
		}
		if mapValue(stageNode, "template") != nil {
			continue // opaque template-included stage; nothing to walk into
		}
		stageName := ""
		if n := mapValue(stageNode, "stage"); n != nil {
			stageName = n.Value
		}
		stagePool := mapValue(stageNode, "pool")
		if stagePool == nil {
			stagePool = rootPool
		}
		if jobsNode := mapValue(stageNode, "jobs"); jobsNode != nil {
			parseJobs(jobsNode, stageName, stagePool, p)
		}
	}
}

func parseJobs(jobsNode *yaml.Node, stageName string, defaultPool *yaml.Node, p *model.Pipeline) {
	if jobsNode.Kind != yaml.SequenceNode {
		return
	}
	for _, jobNode := range FlattenTemplateExpressions(jobsNode) {
		if jobNode.Kind != yaml.MappingNode {
			continue
		}
		if mapValue(jobNode, "template") != nil {
			continue // opaque template-included job
		}

		job := model.Job{StageName: stageName, Line: jobNode.Line, Col: jobNode.Column}
		if n := mapValue(jobNode, "job"); n != nil {
			job.ID = n.Value
			job.Name = n.Value
		}
		if n := mapValue(jobNode, "displayName"); n != nil {
			job.Name = n.Value
		}
		if n := mapValue(jobNode, "condition"); n != nil {
			job.If = n.Value
		}
		if n := mapValue(jobNode, "timeoutInMinutes"); n != nil {
			if v, err := strconv.Atoi(n.Value); err == nil {
				job.TimeoutMinutes = v
			}
		}
		if n := mapValue(jobNode, "dependsOn"); n != nil {
			job.Needs = scalarList(n)
		}
		pool := mapValue(jobNode, "pool")
		if pool == nil {
			pool = defaultPool
		}
		job.RunsOn = poolLabel(pool)
		if stepsNode := mapValue(jobNode, "steps"); stepsNode != nil {
			job.Steps = parseSteps(stepsNode)
		}
		p.Jobs = append(p.Jobs, job)
	}
}

// parseSteps handles every step "kind" Azure schema defines by which
// identifying key is present: checkout, script/bash/powershell/pwsh,
// task, or template. Each maps into the same model.Step shape so
// platform-neutral rules can reason about it via Uses/Run/With/Env
// exactly like a GitHub Actions step.
func parseSteps(stepsNode *yaml.Node) []model.Step {
	if stepsNode.Kind != yaml.SequenceNode {
		return nil
	}
	var out []model.Step
	for _, n := range FlattenTemplateExpressions(stepsNode) {
		if n.Kind != yaml.MappingNode {
			continue
		}
		step := model.Step{Line: n.Line, Col: n.Column}
		if v := mapValue(n, "displayName"); v != nil {
			step.Name = v.Value
		}
		if v := mapValue(n, "condition"); v != nil {
			step.If = v.Value
		}
		if v := mapValue(n, "env"); v != nil {
			step.Env = stringMap(v)
		}

		switch {
		case mapValue(n, "task") != nil:
			step.Uses = mapValue(n, "task").Value
			if v := mapValue(n, "inputs"); v != nil {
				step.With = stringMap(v)
			}
			if step.Name == "" {
				step.Name = step.Uses
			}
			// A script-running task (CmdLine@2, PowerShell@2, Bash@3,
			// AzureCLI@2, ...) carries its command in inputs.script or
			// inputs.inlineScript rather than a top-level "script:"/
			// "bash:" key. Surfacing it as step.Run too — not instead of
			// step.With, which platform-specific rules like AZR002 still
			// need — means every rule that inspects step.Run (LEAN001,
			// PERF001, SEC016, ...) sees it, the same way it already sees
			// GitHub's "run:" and Azure's shorthand steps, instead of
			// silently going blind on what is, in practice, the more
			// common way an Azure pipeline actually runs a shell command.
			if v, ok := step.With["script"]; ok && v != "" {
				step.Run = v
			} else if v, ok := step.With["inlineScript"]; ok && v != "" {
				step.Run = v
			}
		case mapValue(n, "checkout") != nil:
			step.Uses = "checkout:" + mapValue(n, "checkout").Value
			step.With = stepScalarsExcept(n, "checkout", "displayName", "condition", "env")
			if step.Name == "" {
				step.Name = "checkout"
			}
		case mapValue(n, "template") != nil:
			step.Uses = "template:" + mapValue(n, "template").Value
			if v := mapValue(n, "parameters"); v != nil {
				step.With = stringMap(v)
			}
			if step.Name == "" {
				step.Name = step.Uses
			}
		case mapValue(n, "script") != nil:
			step.Run = mapValue(n, "script").Value
		case mapValue(n, "bash") != nil:
			step.Run = mapValue(n, "bash").Value
		case mapValue(n, "powershell") != nil:
			step.Run = mapValue(n, "powershell").Value
		case mapValue(n, "pwsh") != nil:
			step.Run = mapValue(n, "pwsh").Value
		}

		out = append(out, step)
	}
	return out
}

// stepScalarsExcept flattens every scalar-valued key on a step node
// except the ones named, into a string map — used for "checkout:",
// whose options (fetchDepth, persistCredentials, submodules, ...) sit
// as sibling scalar keys rather than nested under a "with:"/"inputs:".
func stepScalarsExcept(n *yaml.Node, exclude ...string) map[string]string {
	skip := map[string]bool{}
	for _, e := range exclude {
		skip[e] = true
	}
	out := map[string]string{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		key := n.Content[i]
		val := n.Content[i+1]
		if skip[key.Value] || val.Kind != yaml.ScalarNode {
			continue
		}
		out[key.Value] = val.Value
	}
	return out
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

func stringMap(n *yaml.Node) map[string]string {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	out := map[string]string{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		out[n.Content[i].Value] = n.Content[i+1].Value
	}
	return out
}

func scalarList(n *yaml.Node) []string {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.ScalarNode {
		return []string{n.Value}
	}
	if n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, 0, len(n.Content))
	for _, c := range n.Content {
		out = append(out, c.Value)
	}
	return out
}
