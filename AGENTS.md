# CLAUDE.md

## Purpose

`ego` ("the reality check between your agents and `main`") is an LLM-powered pull request review GitHub Action for the `ctrl-research` org (previously `ctrl-research/ai-reviewer`, and before that `ctrl-research/actions/pr-review`). It ships two artifacts that must stay in sync:

- **Composite action**: `action.yaml` at the repo root, consumed as `ctrl-research/ego@<ref>`
- **Reusable workflow**: `.github/workflows/pr-review.yaml` (`on: workflow_call`), consumed as `ctrl-research/ego/.github/workflows/pr-review.yaml@<ref>`

The review logic is a dependency-free Go program (`cmd/ego`, `internal/...`); `action.yaml` only obtains the binary and passes inputs to it as `EGO_*` environment variables.

## Tech stack

- **Go** (version pinned in `.tool-versions`), standard library only — no third-party modules
- **GitHub Actions** composite action + reusable workflow (also runs on Forgejo/Gitea Actions)
- **Renovate** for dependency updates (managers: `asdf`, `docker-compose`, `github-actions`, `gomod`)
- **MIT License**

## Structure

```
.
├── .agents/                  # Agent instructions and skills
├── .github/
│   ├── CODEOWNERS            # @ctrl-research/reviewers
│   ├── renovate-config.js    # Renovate platform config
│   └── workflows/
│       ├── ci.yml            # YAML parse + committed-secrets check
│       ├── pr-review.yaml    # Reusable workflow wrapper for the action
│       ├── release.yaml      # Label-driven SemVer release workflow
│       ├── self-review.yaml  # Reviews this repo's PRs with the PR's own code (uses: ./)
│       └── renovate.yaml     # Renovate workflow
├── .tool-versions            # Pinned language/tool versions (asdf/mise)
├── action.yaml               # Composite action: installs and runs the binary
├── cmd/ego/          # main: CLI flags/env → review.Run → step output
├── internal/
│   ├── forge/                # GitHub / Forgejo API: PR, diff, sticky comment
│   ├── gha/                  # Workflow commands and $GITHUB_OUTPUT
│   ├── httpx/                # No-redirect client, retries, bounded bodies
│   ├── llm/                  # Anthropic Messages / OpenAI Chat Completions
│   └── review/               # Config validation, prompts, pipeline
├── go.mod
├── AGENTS.md                 # Operational expectations for humans and AI agents
├── CONTRIBUTING.md
├── LICENSE
├── README.md                 # Action usage, inputs/outputs, security notes
├── SECURITY.md
└── renovate.json             # Renovate settings
```

## Validation

CI (`.github/workflows/ci.yml`) runs gofmt, `go vet`, `go test -race`, a build, and actionlint, plus the template's YAML-parse and committed-secrets checks. Run the same locally (with the Go version from `.tool-versions`):

```bash
gofmt -l .                # must print nothing
go vet ./...
go test -race ./...
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12
python3 -c "import yaml; yaml.safe_load(open('action.yaml'))"   # actionlint doesn't cover action.yaml
```

Note: the template's YAML check only `echo`s on invalid YAML — it does not fail the job.

## Architecture

- `action.yaml` — composite action wrapper. For a bare `X.Y.Z` `github.action_ref` it downloads `ego_<os>_<arch>[.exe]` from that GitHub release and checks it against `checksums.txt`; for any other ref (or a missing asset) it runs `actions/setup-go` and builds from `github.action_path`. It then runs the binary with every input mapped to an `EGO_*` env var.
- The binary: validate config (`internal/review/config.go`) → fetch PR metadata and diff via the forge REST API (no checkout needed) → build prompts → call the LLM → post a sticky PR comment (identified by the `<!-- pr-review-action -->` marker, updated in place on re-runs) → write the `review` output. Keep the marker stable so existing comments keep being updated.
- `.github/workflows/pr-review.yaml` — reusable workflow wrapper that maps `workflow_call` inputs/secrets onto the composite action (`ctrl-research/ego@main`).

Forge abstraction: `github` (`Authorization: Bearer`, diff via `Accept: application/vnd.github.v3.diff`) vs `forgejo`/`gitea` (`Authorization: token`, diff at `/pulls/<n>.diff`).

Provider abstraction: `anthropic` (Messages API, `x-api-key` + `anthropic-version` headers, response text at `.content[] | select(.type=="text")`) vs `openai`/`openai-compatible` (Chat Completions, `Authorization: Bearer`, response at `.choices[0].message.content`). Local endpoints (Ollama/vLLM) use `openai-compatible` with a required `base-url` and optional key.

When adding inputs, update all layers: `action.yaml` (input + `EGO_*` env), `internal/review/config.go` (flag/env + validation), `.github/workflows/pr-review.yaml`, and the inputs table in `README.md`.

## Conventions

- `.tool-versions` is the single source of truth for language and tool versions. Before building, testing, or running any tooling, check it and use the pinned versions (install via `asdf install` or `mise install`). When adding a new language or tool to the project, pin its version there first — never assume a globally installed version.
- Action files are named `action.yaml` (not `action.yml`); `runs.using: composite`; every step needs `shell: bash`.
- Inputs are kebab-case; secrets are passed to the composite action as inputs (composite actions cannot read `secrets` directly).
- Pass untrusted values (PR titles, inputs) to bash via `env:` blocks, never inline `${{ }}` interpolation in `run:`.
- Keep secrets off the argv: the forge token and LLM key are env-only (no flags). All HTTP goes through `httpx.NewClient`, which never follows redirects.
- Standard library only; adding a Go module dependency needs a clear justification.
- Pin third-party actions to exact versions; Renovate manages bumps.
- Versioning: releases follow [SemVer](https://semver.org/) as bare `X.Y.Z` — no `v` prefix (`1.4.2`, not `v1.4.2`). Bump MAJOR for breaking changes (e.g. removing/renaming inputs or outputs, changing defaults), MINOR for backwards-compatible features, PATCH for fixes.
- Conventional commits (`feat`, `fix`, `chore`, `docs`, `ci`, ...); see CONTRIBUTING.md.
- Branch protection: never push directly to `main`; all changes via PR with review.

## Releases

- Releases are bare `X.Y.Z` tags — no `v` prefix. Consumers pin the action to these tags.
- **Automatic bumps**: when a PR merges to `main`, the next version is derived from the PR label:
  - `major` — breaking changes
  - `minor` — backwards-compatible features
  - `patch` — fixes
  - No label — defaults to a `patch` bump
- **Manual releases**: a specific version may be cut manually by supplying an explicit `X.Y.Z` version via workflow dispatch. This bypasses the label-based bump.
- Release automation lives in `.github/workflows/release.yaml`: it computes the next version, runs tests, cross-compiles the binaries (linux/darwin/windows × amd64/arm64) with `checksums.txt`, tags, and creates the GitHub release with those assets attached. Asset names are part of the contract with `action.yaml` — change both together.
