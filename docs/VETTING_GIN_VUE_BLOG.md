# Vetting run: szluyu99/gin-vue-blog

Third vetting pass, chosen deliberately for contrast: ruff and vite are
both maximally-hardened, zizmor-and-semgrep-running projects that
produced 0 blockers each. This run looks for a repo with weaker CI
hygiene to check that vlotpipe's signal actually separates the two
cases, rather than reporting roughly the same noise everywhere.

## Picking the repo

Rather than pick a strawman (an abandoned repo, a cracked-software
listing, a spam repo — several of which showed up in an initial GitHub
search and were discarded as inappropriate/illegitimate vetting
subjects), the goal was a **legitimate, real project** with a genuine
weak spot. [gin-vue-blog](https://github.com/szluyu99/gin-vue-blog) (a
Go + Vue full-stack blog demo) fits: its `ci.yml` has real substance —
gofmt/vet/test for the Go server, lint/test/build for two frontend
packages via a matrix, Docker smoke tests, and a full docker-compose
end-to-end pass that checks permission invariants and reseed
idempotency. This is not a thin, copy-pasted pipeline. But every single
action reference is pinned to a mutable major-version tag (`@v5`, `@v6`,
`@v4`), and the main `ci.yml` sets no `permissions:` anywhere. That's a
realistic, common intermediate-maturity profile — good functional CI,
weak CI *hygiene* — rather than a contrived worst case.

## Method

Same sparse-checkout approach as the previous two runs:

```
git clone --depth 1 --filter=blob:none --no-checkout https://github.com/szluyu99/gin-vue-blog.git
git sparse-checkout init --cone && git sparse-checkout set .github && git checkout
vlotpipe scan gin-vue-blog/ --format json
```

Only 2 workflow files (`ci.yml`, `pages.yml`).

## Result: sharp contrast confirmed

| | ruff (20 files) | vite (13 files) | gin-vue-blog (2 files) |
| --- | --- | --- | --- |
| Total findings | 93 | 37 | 30 |
| Blockers | 0 | 0 | **12** |
| Findings per file | 4.7 | 2.8 | **15.0** |

`SEC001` (12, unpinned actions), `SEC006` (5, missing
`persist-credentials: false`), `SEC005` (4, missing `permissions:`),
`TIMEOUT001` (6), `STRUCT001` (2), `PERF002` (1).

Every count was hand-verified against the source rather than trusted at
face value:

- **`SEC001` = 12**: exactly matches the total count of `uses:`
  references across both files (5× `actions/checkout@v5`, 1×
  `actions/setup-go@v6`, 2× `actions/setup-node@v5`, 2×
  `pnpm/action-setup@v6`, 1× `actions/upload-pages-artifact@v4`, 1×
  `actions/deploy-pages@v4`) — every one of them is tag-pinned, so every
  one is a genuine finding.
- **`SEC005` = 4, not 6**: fires on all 4 jobs in `ci.yml` (none set
  `permissions:`), correctly stays silent on both jobs in `pages.yml`
  because that file sets workflow-level `permissions: {contents: read,
  pages: write, id-token: write}` — confirms `SEC005`'s
  workflow-level-overrides-job-level logic is working, not just
  coincidentally right.
- **`SEC006` = 5**: one `actions/checkout` per job across both files
  that don't set `persist-credentials` at all (4 in `ci.yml` + 1 in
  `pages.yml`).
- **`PERF002` = 1**: fires on `ci.yml` (`pull_request` trigger, no
  `concurrency:` block); correctly silent on `pages.yml`, which has an
  explicit `concurrency: {group: pages, cancel-in-progress: true}`.
- **`STRUCT001` = 2**: a known, already-documented limitation, not a new
  bug — `ci.yml`'s job IDs (`server`, `frontend`, `docker`, `deploy`)
  don't literally contain "test"/"lint"/"check", even though the file
  genuinely runs `go test` and `pnpm test` as *steps* within those jobs.
  `STRUCT001` only looks at job names, not step contents, which is why
  it's `info` severity and documented as low-confidence in
  `SECURITY_RESEARCH.md`. Same shape of noise seen in both prior runs.

## Takeaway

Three repos in, the tool's signal is doing its job: two disciplined,
scanner-running projects produce zero blockers each; a project with
real functional CI but no SHA-pinning or permission scoping produces 12
blockers from 2 files. That's the actual value proposition — not "finds
bugs no other tool can," but "correctly tells you which of your repos
need attention first," which is exactly what a fleet-wide dashboard
needs to aggregate (see the prototype in `vlotpipe-dashboard/`).
