# LEAN001 — apt-install-runtime

**Severity:** warning · **Category:** Lean pipelines

## What it checks

Flags a `run:` step that uses a distro package manager (`apt-get`,
`apt`, `apk`, `yum`, `dnf`) to install a language runtime or compiler
(Python, Node.js, Go, a JDK, Ruby) rather than a maintained
`actions/setup-*` action or a `container:` image that already has it.

## Why it matters

Installing a runtime from a distro package manager, one run at a time,
means resolving the whole package graph from scratch every single time
— no caching, full network round-trips, and a few extra lines of YAML
to maintain. A `setup-*` action typically brings its own dependency
cache; a container image with the toolchain baked in skips the install
step entirely.

## Examples

**Flagged**:

```yaml
steps:
  - run: |
      sudo apt-get update
      sudo apt-get install -y python3.12 python3-pip
  - run: pip install -r requirements.txt
```

**Fixed** — a cached setup action:

```yaml
steps:
  - uses: actions/setup-python@0b93645e9fea7318ecaed2b359559ac225c90a2b # v5.3.0
    with:
      python-version: "3.12"
      cache: "pip"
  - run: pip install -r requirements.txt
```

Or, if nothing about the job needs installing at CI time at all — the
toolchain baked into the runner image:

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    container: python:3.12-slim
    steps:
      - run: pip install -r requirements.txt
```

## Suppressing

The finding is reported on the `run:` step doing the install:

```yaml
  - run: | # vlotpipe: ignore[LEAN001]
      sudo apt-get update
      sudo apt-get install -y python3.12 python3-pip
```

Reasonable when the runtime is needed only to run a bundled third-party
tool (not to build the project's own code) inside a disposable, nested
container — the suggested `setup-*`/image fix genuinely doesn't apply to
every install this rule flags.
