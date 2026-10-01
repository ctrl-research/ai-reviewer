# CLAUDE.md

## Purpose

`ai-reviewer` is an LLM-powered pull request review GitHub Action for the `ctrl-research` org (previously `ctrl-research/actions/pr-review`). It ships two artifacts that must stay in sync:

- **Composite action**: `action.yaml` at the repo root, consumed as `ctrl-research/ai-reviewer@<ref>`
- **Reusable workflow**: `.github/workflows/pr-review.yaml` (`on: workflow_call`), consumed as `ctrl-research/ai-reviewer/.github/workflows/pr-review.yaml@<ref>`

There is no application code, build step, or test suite — everything is YAML + bash steps using preinstalled runner tools (`jq`, `curl`).

## Tech stack

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
│       └── renovate.yaml     # Renovate workflow
├── .tool-versions            # Pinned language/tool versions (asdf/mise)
├── action.yaml               # Composite action: LLM-powered PR review
├── AGENTS.md                 # Operational expectations for humans and AI agents
├── CONTRIBUTING.md
├── LICENSE
├── README.md                 # Action usage, inputs/outputs, security notes
├── SECURITY.md
└── renovate.json             # Renovate settings
```

## Validation

CI (`.github/workflows/ci.yml`) only checks YAML parseability and greps for committed secrets. Validate locally before pushing:

```bash
# YAML syntax (what CI does)
python3 -c "import yaml; yaml.safe_load(open('action.yaml'))"

# Workflow lint (if installed)
actionlint .github/workflows/pr-review.yaml
```

Note: the CI YAML check only `echo`s on invalid YAML — it does not fail the job. Don't rely on CI to catch syntax errors.

## Architecture

- `action.yaml` — composite action. Pure bash/jq/curl pipeline: validate inputs → fetch PR metadata and diff via the forge REST API (no checkout needed) → build prompts → call the LLM → post a sticky PR comment (identified by the `<!-- pr-review-action -->` marker, updated in place on re-runs). Keep the marker stable so existing comments keep being updated.
- `.github/workflows/pr-review.yaml` — reusable workflow wrapper that maps `workflow_call` inputs/secrets onto the composite action (`ctrl-research/ai-reviewer@main`).

Forge abstraction: `github` (`Authorization: Bearer`, diff via `Accept: application/vnd.github.v3.diff`) vs `forgejo`/`gitea` (`Authorization: token`, diff at `/pulls/<n>.diff`).

Provider abstraction: `anthropic` (Messages API, `x-api-key` + `anthropic-version` headers, response text at `.content[] | select(.type=="text")`) vs `openai`/`openai-compatible` (Chat Completions, `Authorization: Bearer`, response at `.choices[0].message.content`). Local endpoints (Ollama/vLLM) use `openai-compatible` with a required `base-url` and optional key.

When adding inputs, update both layers and the inputs table in `README.md`.

## Conventions

- `.tool-versions` is the single source of truth for language and tool versions. Before building, testing, or running any tooling, check it and use the pinned versions (install via `asdf install` or `mise install`). When adding a new language or tool to the project, pin its version there first — never assume a globally installed version.
- Action files are named `action.yaml` (not `action.yml`); `runs.using: composite`; every step needs `shell: bash`.
- Inputs are kebab-case; secrets are passed to the composite action as inputs (composite actions cannot read `secrets` directly).
- Pass untrusted values (PR titles, inputs) to bash via `env:` blocks, never inline `${{ }}` interpolation in `run:`.
- Keep secrets off the argv: auth headers go in 0600 curl config files; never follow redirects with the forge token.
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
- Release automation lives in `.github/workflows/release.yaml`: it computes the next version, tags, and creates the GitHub release. The action is consumed straight from the tag, so there are no artifacts to build or publish.
