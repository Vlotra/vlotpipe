// Package model defines the normalized pipeline representation that every
// platform parser (GitHub Actions, Azure Pipelines, ...) maps into. Rules
// only ever operate on this model, never on the raw platform YAML.
package model

// Platform identifies which CI system a Pipeline was parsed from.
type Platform string

const (
	PlatformGitHubActions  Platform = "github"
	PlatformAzurePipelines Platform = "azure"
)

// Pipeline is one parsed workflow/pipeline file.
type Pipeline struct {
	Platform Platform
	Path     string
	Name     string
	// OnEvents is the set of trigger event names in the "on:" block, e.g.
	// "push", "pull_request", "pull_request_target", "workflow_run".
	OnEvents []string
	// PermissionsSet is true if a workflow-level "permissions:" key is
	// present at all (even as "permissions: {}"), distinct from Permissions
	// being empty because no scopes were granted.
	PermissionsSet bool
	Permissions    map[string]string
	// Concurrency is true if a top-level "concurrency:" key is present.
	Concurrency bool
	Jobs        []Job
	// Suppressions maps a 1-indexed line number to the rule codes an inline
	// "# vlotpipe: ignore[CODE,...]" comment on that line suppresses. A
	// present key with an empty slice means a bare "# vlotpipe: ignore"
	// with no code list, which suppresses every code on that line.
	Suppressions map[int][]string
}

// HasTrigger reports whether the workflow is triggered by the named event.
func (p *Pipeline) HasTrigger(event string) bool {
	for _, e := range p.OnEvents {
		if e == event {
			return true
		}
	}
	return false
}

// IsSuppressed reports whether an inline "# vlotpipe: ignore" comment on
// line suppresses code.
func (p *Pipeline) IsSuppressed(line int, code string) bool {
	codes, ok := p.Suppressions[line]
	if !ok {
		return false
	}
	if len(codes) == 0 {
		return true
	}
	for _, c := range codes {
		if c == code {
			return true
		}
	}
	return false
}

// Container describes a job's "container:" or "services.<name>:" spec.
type Container struct {
	Image    string
	Username string
	Password string
	Line     int
	Col      int
}

// Job is a single unit of work that runs on one runner (a GitHub "job" or
// an Azure "job" inside a stage).
type Job struct {
	ID   string
	Name string
	// StageName is set for Azure Pipelines jobs that belong to an explicit
	// "stages:" block, for provenance in messages. Empty for GitHub
	// Actions jobs (which have no stage concept) and for Azure pipelines
	// with no explicit stages (an implicit single stage).
	StageName      string
	RunsOn         string
	If             string
	Needs          []string
	TimeoutMinutes int // 0 means "not set"
	PermissionsSet bool
	Permissions    map[string]string
	// Uses is set when this job calls a reusable workflow instead of
	// running its own steps, e.g. "./.github/workflows/build.yml".
	Uses string
	// SecretsInherit is true when the reusable workflow call uses
	// "secrets: inherit" instead of forwarding secrets individually.
	SecretsInherit bool
	// SecretsLine/SecretsCol point at the "secrets:" value itself (not the
	// job's own declaration line), since that's where a suppression
	// comment naturally goes — e.g. "secrets: inherit # vlotpipe: ignore[SEC007]".
	SecretsLine int
	SecretsCol  int
	Container   *Container
	Services    map[string]*Container
	Steps       []Step
	Line        int
	Col         int
}

// Step is a single action/task/script invocation within a Job.
type Step struct {
	Name string
	Uses string // e.g. "actions/checkout@v4"
	Run  string
	If   string
	With map[string]string
	Env  map[string]string
	Line int
	Col  int
}
