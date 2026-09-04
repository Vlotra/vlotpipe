# Vetting run: orbingol/aitos

Fourth vetting pass, user-requested (`https://github.com/orbingol/aitos/actions`).
A small, single-maintainer JavaScript/shell project (an experimental ATS
resume/CV analyzer, CLI + web UI) — useful as a contrast point to
`gin-vue-blog`: another small, casual-scale repo, but with the opposite
CI-hygiene profile.

## Method

```
git clone --depth 1 --filter=blob:none --no-checkout https://github.com/orbingol/aitos.git
git sparse-checkout init --cone && git sparse-checkout set .github && git checkout
vlotpipe scan aitos/ -s
```

4 workflow files: `tests-cli.yml`, `tests-cli-docker.yml`, `tests-app.yml`,
`release-cli.yml`.

## Result: zero blockers, unlike gin-vue-blog

```
4 files scanned, 20 violations found

By severity
  blocker   0
  warning   14
  info      6

By rule
  ● TIMEOUT001  5
  ● SEC006      4
  ● PERF002     3
  ● SEC005      3
  ● STRUCT001   2
  ● LEAN001     1
  ● PERF003     1
  ● SUPPLY001   1
```

The headline: **every third-party action is already SHA-pinned with a
version comment** (`actions/checkout@de0fac2e...  # v6.0.2`,
`svenstaro/upload-release-action@29e53e91...  # v2.11.5`), so `SEC001`
never fires — same clean result as ruff and vite got on this specific
check, from a repo with zero stars and one maintainer. SHA-pinning isn't
correlated with project size or fame; it's a specific habit someone
either has or doesn't.

## Spot-checks

- **`SUPPLY001`**: confirmed — no `dependabot.yml` or `renovate.json`
  anywhere in the repo, so a pinned action here has nothing keeping it
  current.
- **`PERF003`**: confirmed real usage of `svenstaro/upload-release-action`
  in `release-cli.yml` for uploading release assets; `gh release upload`
  does the same job without the extra dependency.
- **No dangerous triggers, no secret misuse**: grepped
  `pull_request_target|workflow_run|issue_comment|self-hosted|secrets\.`
  across all four files directly — nothing beyond the expected
  `secrets.GITHUB_TOKEN` passed to the upload-release-action step, which
  is exactly what that token is for.

### `LEAN001`'s one nuanced hit

Fired on `tests-cli.yml`:

```yaml
- name: Install dependencies
  run: |
    sudo apt-get update
    sudo apt-get install -y openjdk-17-jre-headless poppler-utils enscript ghostscript jq
```

Worth being precise about *why* this is installed: the project isn't
Java, and this isn't a build toolchain — `openjdk-17-jre-headless` is
installed purely so a later step can run `tika-app.jar` (Apache Tika, a
third-party document-extraction tool the tests shell out to). `LEAN001`'s
message ("use the matching setup-* action... or a container image") is
generic and doesn't capture that specific "why," but the underlying
suggestion still holds regardless of motive: `actions/setup-java` brings
its own dependency cache, so swapping the `apt-get install` for it would
still be faster on every run after the first, even though the reason
Java is needed here has nothing to do with building this project's own
code. Filed as a nuance, not a bug — distinct from the ruff `docker run
alpine apk add python3` case (`VETTING_RUFF.md`), which really was
testing inside a disposable container LEAN001 has no way to reason
about; here the install genuinely happens on the job's own runner and
genuinely would benefit from the suggested fix.

## Takeaway

Four repos in, a pattern is holding: SHA-pinning tracks individual habit,
not project maturity or fame. `gin-vue-blog` (real functional test/e2e
coverage, backed by a team) had 12 blockers from tag-only pinning across
2 files; `aitos` (zero stars, one maintainer, side project) has zero,
because whoever wrote its workflows happened to pin correctly from the
start. What both share — and what ruff and vite also share, despite
being maximally hardened on the security axis — is the cheaper hygiene
gaps: no `timeout-minutes`, no `concurrency:` groups, no
`persist-credentials: false`. Those four rules (`TIMEOUT001`, `PERF002`,
`SEC005`/`SEC006`) are the closest thing to a universal finding across
every repo vetted so far, regardless of size, language, or team.
