---
title: Releases and development
tags:
  - reference
---

## Versions

Releases are tagged with bare SemVer (`X.Y.Z`, no `v` prefix), and each release ships prebuilt binaries for Linux, macOS and Windows on amd64 and arm64, with SHA-256 checksums.

- **Pin a release** (`ctrl-research/ego@X.Y.Z`) for reproducible reviews and the prebuilt binary.
- **`@main`** always has the latest changes, and builds from source on every run.
- **The reusable workflow** always calls the action at `@main`. Use the composite action directly if you need the whole pipeline pinned.

## How releases are cut

Merging a PR to `main` cuts a release. The PR's label decides the bump:

| Label | Bump | Use for |
|---|---|---|
| `major` | `X.0.0` | Breaking changes, e.g. removing or renaming inputs or outputs, or changing defaults |
| `minor` | `0.X.0` | Backwards-compatible features |
| `patch` (or no label) | `0.0.X` | Fixes, docs and dependency updates |

Renovate labels its PRs `update:major`, `update:minor` and `update:patch` (the *dependency's* update type). Those don't affect `ego`'s version, so dependency bumps release as a patch unless a maintainer adds a release label.

A specific version can also be released by running the **Release** workflow manually with an explicit `X.Y.Z`.

## Codebase

`ego` is a dependency-free Go program (standard library only). `action.yaml` only obtains the binary and passes the inputs to it as `EGO_*` environment variables.

| Path | What it does |
|---|---|
| `cmd/ego` | Entry point: settings, run the review, write the step outputs |
| `internal/review` | Validates configuration, builds the prompt, runs the pipeline |
| `internal/report` | The review's JSON schema, the lenient parser, the markdown renderer and price table |
| `internal/llm` | Anthropic Messages and OpenAI Chat Completions clients |
| `internal/forge` | GitHub and Forgejo APIs: the PR, its diff, the sticky comment |
| `internal/httpx` | HTTP client that never follows redirects, retries, bounded response bodies |
| `internal/gha` | Workflow commands and `$GITHUB_OUTPUT` |
| `examples/` | The workflows behind the [recipes](recipes/index), linted in CI |
| `docs/` | This site, built with [nebula-md](https://github.com/ctrl-research/nebula-md) |

Contributor and agent guidelines are in [`AGENTS.md`](https://github.com/ctrl-research/ego/blob/main/AGENTS.md) and [`CONTRIBUTING.md`](https://github.com/ctrl-research/ego/blob/main/CONTRIBUTING.md).

## Building the docs

```bash
# Build nebula-md at the commit pinned in .github/workflows/docs.yaml.
git clone https://github.com/ctrl-research/nebula-md /tmp/nebula-md
git -C /tmp/nebula-md checkout <NEBULA_REF from docs.yaml>
(cd /tmp/nebula-md/src && go build -o /tmp/nebula .)

NEBULA_BIN=/tmp/nebula scripts/build-docs.sh site
```

Then open `site/index.html`, or serve the folder with any static file server. Pushes to `main` that touch `docs/` or `examples/` rebuild and publish the site to GitHub Pages.
