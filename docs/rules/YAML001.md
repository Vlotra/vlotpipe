# YAML001 — invalid-yaml

**Severity:** blocker · **Category:** YAML · **Platforms:** GitHub Actions, Azure Pipelines

## What it checks

Flags a pipeline file that isn't valid YAML at all — a stray tab used
for indentation (YAML disallows tabs in indentation entirely), an
unclosed `{`/`[`, mismatched quotes, or anything else the parser itself
rejects.

## Why it matters

Before this rule existed, a file that failed to parse printed an error
to stderr and was silently skipped from the rest of the scan —
`vlotpipe check` still exited 0 and reported "All checks passed!",
exactly backwards for a CI gate whose entire job is to catch broken
pipeline files. This is the one rule in the whole tool that can't be
argued with: if the YAML doesn't parse, the platform running it won't
be able to either.

## Examples

**Flagged** — a tab character where YAML requires spaces:

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
	  extra: this line is indented with a tab
```

**Flagged** — an unclosed flow mapping:

```yaml
steps:
  - run: echo hi
    with: {foo: unclosed
```

**Fixed**: whatever the parser's specific complaint is, addressed —
there's no generic template for "valid YAML," only the specific syntax
error reported in the message.

## Suppressing

No inline `# vlotpipe: ignore` comment — there's no parsed tree to
attach one to when parsing itself is what failed. `.vlotpipe.yml` still
works, though it's rarely the right tool here: a file that doesn't parse
isn't a policy disagreement to file an exception for, it's broken.

```yaml
ignore:
  - code: YAML001
    path: "vendor/generated-pipeline.yml"
    reason: "generated file with a known, tolerated templating artifact; upstream issue filed"
```
