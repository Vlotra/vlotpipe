# CI/CD Pipeline Security Research — Rule Category Roadmap

Research pass for expanding vlotpipe's rule pack beyond the v0.1 baseline
(`SEC001/002`, `PERF001`, `TIMEOUT001`, `STRUCT001`). Sources: OWASP Top 10
CI/CD Security Risks, OWASP CI/CD & GitHub Actions Cheat Sheets, GitHub's
own security-hardening docs, [zizmor](https://docs.zizmor.sh/audits/)
(the most mature GitHub Actions static analyzer — 35+ audits, Rust,
MIT-licensed), OpenSSF Scorecard's 18 checks, SLSA, NIST SSDF, and Microsoft
Learn's Azure Pipelines security docs. Real-world grounding from the
tj-actions/changed-files (CVE-2025-30066) and reviewdog/action-setup
(CVE-2025-30154) chained supply-chain attack, March 2025.

See also `LEAN_PIPELINES_RESEARCH.md` for the build-time/pipeline-size
counterpart to this doc — a `LEAN` category covering short pipeline files
and short build times, plus two items (`PERF002` concurrency limits,
`PERF003` superfluous actions) that turned out to belong under `PERF`
below rather than in `LEAN`.

## 1. Industry frameworks (the "why")

**OWASP Top 10 CI/CD Security Risks** — the risk taxonomy vlotpipe's rule
categories should map onto:

| ID | Risk |
| --- | --- |
| CICD-SEC-1 | Insufficient Flow Control Mechanisms |
| CICD-SEC-2 | Inadequate Identity and Access Management |
| CICD-SEC-3 | Dependency Chain Abuse |
| CICD-SEC-4 | Poisoned Pipeline Execution (PPE) |
| CICD-SEC-5 | Insufficient Pipeline-Based Access Controls |
| CICD-SEC-6 | Insufficient Credential Hygiene |
| CICD-SEC-7 | Insecure System Configuration |
| CICD-SEC-8 | Ungoverned Usage of Third-Party Services |
| CICD-SEC-9 | Improper Artifact Integrity Validation |
| CICD-SEC-10 | Insufficient Logging and Visibility |

**SLSA** (slsa.dev) — four levels of build-provenance assurance, from "have
a documented build process" (L1) to "hermetic, verifiable, non-falsifiable
provenance" (L4). vlotpipe can check for the *inputs* to SLSA compliance
(provenance generation, hash-pinned dependencies) without itself being an
attestation verifier.

**OpenSSF Scorecard** (18 automated checks) — the closest existing tool to
vlotpipe's ambition, but repo-wide rather than pipeline-file-focused:
Binary-Artifacts, Branch-Protection, CI-Tests, CII-Best-Practices,
Code-Review, Contributors, Dangerous-Workflow, Dependency-Update-Tool,
Fuzzing, License, Maintained, Packaging, Pinned-Dependencies, SAST, SBOM,
Security-Policy, Signed-Releases, Token-Permissions, Vulnerabilities,
Webhooks. Several of these (Dangerous-Workflow, Token-Permissions,
Pinned-Dependencies) are exactly vlotpipe's territory and worth using as a
cross-check for false-negative rate once vlotpipe has more rules.

**NIST SSDF (SP 800-218)** — the federal secure-software-development
framework; relevant if vlotpipe wants a "compliance" positioning (e.g. an
`--attest ssdf` flag mapping findings to SSDF practice IDs) for the paid
tier's compliance-reporting angle.

**Azure DevOps** has no equivalent public community scanner as mature as
zizmor — Microsoft Learn's own hardening guide is the primary source, which
is a gap vlotpipe's "ruff for pipelines, GitHub *and* Azure" positioning
can exploit.

## 2. Real-world incident: tj-actions/changed-files (CVE-2025-30066)

March 2025: attacker compromised a maintainer bot's GitHub PAT, then
**retroactively rewrote every version tag** (`v1` … `v45`) on
`tj-actions/changed-files` to point at a malicious commit that dumped CI
runner memory (secrets) into build logs, base64-encoded. 23,000+ public
repos were affected; only repos that had pinned to a **full commit SHA**
were unaffected — tag-pinned repos (`@v45`, `@v4`, etc.) were all
compromised simultaneously, retroactively, with no code change on their
end. A follow-on investigation found `reviewdog/action-setup@v1` had been
compromised earlier, likely providing the initial foothold (CVE-2025-30154)
— a **chained** supply-chain attack. This is the single strongest argument
for treating `SEC001` (unpinned action) as blocker-severity by default:
it's not hypothetical, it hit tens of thousands of repos in one day.

## 3. Rule category taxonomy

vlotpipe's namespace is `<CATEGORY><NNN>`. Proposal: keep `SEC`/`PERF`/
`STRUCT`/`TIMEOUT`, add two new categories — `SUPPLY` (supply-chain
integrity, distinct from general security) and `GOV` (governance/
compliance, repo-level rather than workflow-level). Zizmor rule names are
listed in `()` where vlotpipe would be re-implementing a known, battle-tested
check rather than inventing a new one.

### SEC — Security (expand existing category)

| Code (proposed) | Severity | Checks | Source |
| --- | --- | --- | --- |
| SEC001 ✅ implemented | blocker | action not pinned to full commit SHA | tj-actions incident, zizmor `unpinned-uses`, Scorecard Pinned-Dependencies |
| SEC002 ✅ implemented | blocker | hardcoded credential in `with`/`env` | OWASP CI/CD Cheat Sheet |
| SEC003 ✅ implemented | blocker | `pull_request_target` or `workflow_run` trigger combined with explicit checkout of PR head | zizmor `dangerous-triggers`, GitHub Security Lab "pwn requests", Scorecard Dangerous-Workflow |
| SEC004 ✅ implemented | blocker | untrusted context (`github.event.issue.title`, PR title/body, etc.) interpolated directly into `run:` instead of via env var | zizmor `template-injection`, GitHub's script-injection guidance |
| SEC005 ✅ implemented | warning | workflow- or job-level `permissions` not set (relies on GitHub's broad default `GITHUB_TOKEN` scope) | zizmor `excessive-permissions`, Scorecard Token-Permissions |
| SEC006 ✅ implemented | warning | `actions/checkout` used without `persist-credentials: false` when git push/write isn't needed | zizmor `artipacked` (ArtiPACKED credential leak via artifacts) |
| SEC007 ✅ implemented | warning | `secrets: inherit` on a reusable workflow call instead of explicit per-secret forwarding | zizmor `secrets-inherit` |
| SEC008 ✅ implemented | warning | entire `secrets` context dumped via `toJSON(secrets)` instead of naming individual secrets | zizmor `overprovisioned-secrets` |
| SEC009 | info | action pinned to a mutable branch/tag ref rather than SHA even when a comment states a version (ref-confusion — an attacker-controlled ref can shadow it) | zizmor `ref-confusion`, `impostor-commit` |
| SEC010 ✅ implemented | warning | self-hosted runner referenced in a workflow that also accepts `pull_request` from forks | OWASP GH Actions Cheat Sheet, zizmor `self-hosted-runner` |
| SEC011 ✅ implemented | warning | `github.actor`-only bot/authorization check (spoofable) instead of `github.event.pull_request.user.login` | zizmor `bot-conditions` |
| SEC012 ✅ implemented | blocker | write to `GITHUB_ENV`/`GITHUB_PATH` from attacker-controlled data inside a privileged-trigger workflow | zizmor `github-env` |
| SEC013 ✅ implemented | warning | `ACTIONS_ALLOW_UNSECURE_COMMANDS` set (re-enables deprecated, injectable `::set-env`/`::add-path`) | zizmor `insecure-commands` |
| SEC014 ✅ implemented | blocker | Docker `container:`/`services:` credentials hardcoded instead of via secrets | zizmor `hardcoded-container-credentials` |
| SEC015 ✅ implemented | warning | release/publish workflow uses `actions/cache` or a setup-action's built-in cache without disabling it for the release trigger (cache poisoning → compromised release artifact) | zizmor `cache-poisoning`, OWASP GH Actions Cheat Sheet |
| SEC016 | info | `uses:` slug is a one-edit-distance typo of a well-known action under a different owner (typosquat) | zizmor `typosquat-uses` |

### SUPPLY — Supply-chain integrity (new category)

| Code | Severity | Checks | Source |
| --- | --- | --- | --- |
| SUPPLY001 ✅ implemented | warning | no Dependabot/Renovate config present for keeping pinned actions up to date | Scorecard Dependency-Update-Tool, GitHub docs |
| SUPPLY002 | info | Dependabot/Renovate has no cooldown/`minimumReleaseAge`, so newly-published (possibly still-compromised) versions get pulled immediately | OWASP GH Actions Cheat Sheet |
| SUPPLY003 | warning | release/publish job pushes to a registry (npm/PyPI/container) using a static long-lived token instead of OIDC "trusted publishing" | zizmor `use-trusted-publishing`, GitHub docs |
| SUPPLY004 | info | no SLSA provenance or SBOM (CycloneDX/SPDX) generated for release artifacts | SLSA.dev, Scorecard SBOM check |
| SUPPLY005 | info | release job has no signature step (`cosign`/`sigstore`) for published artifacts | Scorecard Signed-Releases, in-toto/Sigstore |
| SUPPLY006 | warning | GitHub App installation token (`actions/create-github-app-token`) issued with org-wide `owner:` scope instead of a specific `repositories:` list, or `skip-token-revoke: true` | zizmor `github-app` |

### GOV — Governance & compliance (new category, repo-level)

These need repo metadata beyond workflow YAML (branch protection API,
`SECURITY.md`, `CODEOWNERS`) — a larger lift than parsing a workflow file,
worth scoping as a v2 milestone requiring GitHub API access, not just local
file parsing.

| Code | Severity | Checks | Source |
| --- | --- | --- | --- |
| GOV001 | warning | default branch lacks required-review / status-check branch protection | Scorecard Branch-Protection, OWASP CI/CD Cheat Sheet |
| GOV002 | info | no `SECURITY.md` vulnerability-disclosure policy | Scorecard Security-Policy |
| GOV003 | info | `.github/workflows/` not covered by a `CODEOWNERS` entry (workflow changes can merge without a designated reviewer) | GitHub docs "Using CODEOWNERS to monitor changes" |
| GOV004 | warning | "Require approval for first-time contributors" enabled instead of "for all external contributors" (trust-then-betray gap) | OWASP GH Actions Cheat Sheet |

### PERF — Performance (expand existing category)

| Code | Severity | Checks | Source |
| --- | --- | --- | --- |
| PERF001 ✅ implemented | warning | dependency install without caching | — |
| PERF002 ✅ implemented | info | no `concurrency:`/`cancel-in-progress` group, so superseded runs keep burning runner minutes | zizmor `concurrency-limits` |
| PERF003 ✅ implemented | info | action wraps a CLI already preinstalled on the runner image (e.g. `dtolnay/rust-toolchain` vs `rustup`, `peter-evans/create-pull-request` vs `gh pr create`) — extra supply-chain surface for no benefit | zizmor `superfluous-actions` |

### STRUCT — Structure (expand existing category)

STRUCT001 (missing test/lint job) already implemented. Candidate
additions once the Azure parser exists: STRUCT002 missing security-scan
stage, STRUCT003 no required-status-check gate before deploy.

## 4. GitHub Actions → Azure Pipelines equivalents

Useful for keeping the rule pack conceptually unified once the Azure
parser lands (spec section 4):

| Concept | GitHub Actions | Azure Pipelines |
| --- | --- | --- |
| Untrusted-fork execution | `pull_request_target` | fork builds + "make secrets available to builds of forks" (should be off) |
| Job token scoping | `permissions:` on `GITHUB_TOKEN` | job authorization scope, project-level vs. collection-level identities |
| Long-lived cloud creds | OIDC "trusted publishing" | workload identity federation on the service connection |
| Runner isolation | self-hosted runner hardening | Microsoft-hosted vs. self-hosted agents, agent pool segmentation |
| Untrusted input → shell injection | context interpolation into `run:` | queue-time variables / shell task argument validation setting |
| Dependency pinning | SHA-pinned `uses:` | template/task version pinning, `extends` templates |

## 5. Suggested implementation priority

Ranked by real-world exploitation frequency and how directly each traces
to a documented incident, not just theoretical risk:

1. **SEC003** (`pull_request_target`/`workflow_run` + untrusted checkout) —
   the single most-exploited GitHub Actions pattern per GitHub Security Lab.
2. **SEC005** (missing/default `permissions:`) — Scorecard's highest-risk
   check besides Dangerous-Workflow; nearly every workflow is affected by
   default.
3. **SEC004** (template injection into `run:`) — the mechanism behind most
   "PR title/body used for RCE" disclosures.
4. **SEC007/SEC008** (`secrets: inherit` / `toJSON(secrets)`) — cheap to
   detect (pure YAML pattern), directly reduces blast radius of exactly
   the kind of leak tj-actions caused.
5. **SUPPLY001** (no Dependabot/Renovate for actions) — closes the loop on
   SEC001: pinning to a SHA is only durable if something keeps that SHA
   current.
6. **SEC006** (missing `persist-credentials: false`) and **SEC015**
   (cache poisoning in release workflows) — both cheap AST checks, both
   tied to named CVEs/write-ups (ArtiPACKED, Cacheract).

Items 1–4 are all pure `internal/model` pattern checks — no new parser
capability needed, same shape as the existing `SEC001`/`TIMEOUT001` rules.
GOV* items require GitHub REST API calls (branch protection settings
aren't in the workflow YAML), which is a bigger architectural addition —
worth deferring to a `vlotpipe scan --github-api` mode rather than blocking
the local-file-only v1.
