// Package github parses GitHub Actions workflow files into the normalized
// internal/model representation.
package github

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

// Detect reports whether path looks like a GitHub Actions workflow file.
func Detect(path string) bool {
	slash := filepath.ToSlash(path)
	return strings.Contains(slash, ".github/workflows/") &&
		(strings.HasSuffix(slash, ".yml") || strings.HasSuffix(slash, ".yaml"))
}

// ParseFile reads and parses a single workflow file.
func ParseFile(path string) (*model.Pipeline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Parse(path, data)
}

// Parse parses workflow YAML into a model.Pipeline, preserving line/column
// info so rules can report precise locations.
func Parse(path string, data []byte) (*model.Pipeline, error) {
	p := &model.Pipeline{Platform: model.PlatformGitHubActions, Path: path}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	p.Suppressions = suppress.Parse(&root)
	if len(root.Content) == 0 {
		return p, nil
	}
	doc := root.Content[0]

	if n := mapValue(doc, "name"); n != nil {
		p.Name = n.Value
	}
	if n := mapValue(doc, "on"); n != nil {
		p.OnEvents = parseOnEvents(n)
	}
	if n := mapValue(doc, "permissions"); n != nil {
		p.PermissionsSet = true
		p.Permissions = stringMap(n)
	}
	if n := mapValue(doc, "concurrency"); n != nil {
		p.Concurrency = true
	}

	jobsNode := mapValue(doc, "jobs")
	if jobsNode == nil || jobsNode.Kind != yaml.MappingNode {
		return p, nil
	}

	for i := 0; i+1 < len(jobsNode.Content); i += 2 {
		idNode := jobsNode.Content[i]
		jobNode := jobsNode.Content[i+1]

		job := model.Job{ID: idNode.Value, Name: idNode.Value, Line: idNode.Line, Col: idNode.Column}
		if jobNode.Kind == yaml.MappingNode {
			if n := mapValue(jobNode, "name"); n != nil {
				job.Name = n.Value
			}
			if n := mapValue(jobNode, "runs-on"); n != nil {
				job.RunsOn = n.Value
			}
			if n := mapValue(jobNode, "if"); n != nil {
				job.If = n.Value
			}
			if n := mapValue(jobNode, "timeout-minutes"); n != nil {
				if v, err := strconv.Atoi(n.Value); err == nil {
					job.TimeoutMinutes = v
				}
			}
			if n := mapValue(jobNode, "needs"); n != nil {
				job.Needs = scalarList(n)
			}
			if n := mapValue(jobNode, "permissions"); n != nil {
				job.PermissionsSet = true
				job.Permissions = stringMap(n)
			}
			if n := mapValue(jobNode, "uses"); n != nil {
				job.Uses = n.Value
			}
			if n := mapValue(jobNode, "secrets"); n != nil && n.Kind == yaml.ScalarNode && n.Value == "inherit" {
				job.SecretsInherit = true
				job.SecretsLine = n.Line
				job.SecretsCol = n.Column
			}
			if n := mapValue(jobNode, "container"); n != nil {
				job.Container = parseContainer(n)
			}
			if n := mapValue(jobNode, "services"); n != nil && n.Kind == yaml.MappingNode {
				job.Services = map[string]*model.Container{}
				for i := 0; i+1 < len(n.Content); i += 2 {
					job.Services[n.Content[i].Value] = parseContainer(n.Content[i+1])
				}
			}
			if n := mapValue(jobNode, "steps"); n != nil && n.Kind == yaml.SequenceNode {
				for _, stepNode := range n.Content {
					job.Steps = append(job.Steps, parseStep(stepNode))
				}
			}
		}
		p.Jobs = append(p.Jobs, job)
	}

	return p, nil
}

// parseOnEvents normalizes the "on:" block, which GitHub Actions allows as
// a single event string, a list of event strings, or a map of event name
// to its trigger config, into a flat list of event names.
func parseOnEvents(n *yaml.Node) []string {
	switch n.Kind {
	case yaml.ScalarNode:
		return []string{n.Value}
	case yaml.SequenceNode:
		return scalarList(n)
	case yaml.MappingNode:
		out := make([]string, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, n.Content[i].Value)
		}
		return out
	default:
		return nil
	}
}

// parseContainer handles both the shorthand "container: image:tag" form and
// the full "container: { image, credentials }" mapping form used by both
// job.container and job.services.<name>.
func parseContainer(n *yaml.Node) *model.Container {
	c := &model.Container{Line: n.Line, Col: n.Column}
	if n.Kind == yaml.ScalarNode {
		c.Image = n.Value
		return c
	}
	if n.Kind != yaml.MappingNode {
		return c
	}
	if v := mapValue(n, "image"); v != nil {
		c.Image = v.Value
	}
	if creds := mapValue(n, "credentials"); creds != nil {
		if v := mapValue(creds, "username"); v != nil {
			c.Username = v.Value
		}
		if v := mapValue(creds, "password"); v != nil {
			c.Password = v.Value
		}
	}
	return c
}

func parseStep(n *yaml.Node) model.Step {
	step := model.Step{Line: n.Line, Col: n.Column}
	if n.Kind != yaml.MappingNode {
		return step
	}
	if v := mapValue(n, "name"); v != nil {
		step.Name = v.Value
	}
	if v := mapValue(n, "uses"); v != nil {
		step.Uses = v.Value
	}
	if v := mapValue(n, "run"); v != nil {
		step.Run = v.Value
	}
	if v := mapValue(n, "if"); v != nil {
		step.If = v.Value
	}
	if v := mapValue(n, "with"); v != nil {
		step.With = stringMap(v)
	}
	if v := mapValue(n, "env"); v != nil {
		step.Env = stringMap(v)
	}
	return step
}

// mapValue returns the value node for key in a YAML mapping node, or nil.
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
