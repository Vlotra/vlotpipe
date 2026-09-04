# SUPPLY001 — no-dependency-update-tool

**Severity:** warning · **Category:** Supply chain

## What it checks

This one is different from every other rule: it's a **repo-level**
check, not a per-workflow one. If any scanned workflow references a
third-party action, and the repo has no `.github/dependabot.yml`,
`renovate.json`, or equivalent config anywhere, it fires once for the
whole scan.

## Why it matters

`SEC001` requires pinning actions to a commit SHA — but a SHA pin is
only as secure as how current it is. Without Dependabot or Renovate
configured for the `github-actions` ecosystem, a correctly-pinned action
just quietly stops receiving security patches; nothing updates it unless
a human remembers to.

## Examples

**Flagged** — no update tooling anywhere in the repo, while workflows
reference several third-party actions:

```
myrepo/
├── .github/
│   └── workflows/
│       └── ci.yml   # uses: actions/checkout@..., some/action@...
```

**Fixed** — add a Dependabot config covering the `github-actions`
ecosystem:

```yaml
# .github/dependabot.yml
version: 2
updates:
  - package-ecosystem: "github-actions"
    directory: "/"
    schedule:
      interval: "weekly"
```

Renovate works too, and either tool supports a cooldown/minimum-release-age
setting worth turning on — it delays picking up a newly-published action
version by a few days, giving the community time to catch a compromised
release before it lands in your workflows automatically.

## Suppressing

Since this finding isn't tied to a specific line in a specific file, use
`.vlotpipe.yml` rather than an inline comment:

```yaml
ignore:
  - code: SUPPLY001
    path: "*"
    reason: "actions updated manually on a monthly review cadence"
```
