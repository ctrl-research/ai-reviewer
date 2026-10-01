# CLAUDE.md

## Purpose

GitHub template repository for bootstrapping `ctrl-research` projects. Provides Renovate-managed dependency updates, branch protection, CODEOWNERS, license, and standard `.gitignore` as a starting point — not a runnable application.

## Tech stack

- **Renovate** for dependency updates (managers: `docker-compose`, `github-actions`, `gomod`)
- **GitHub Actions** for the Renovate workflow
- **MIT License**

## Structure

```
.
├── .agents/                  # Agent instructions and skills
├── .github/
│   ├── CODEOWNERS            # @ctrl-research/reviewers
│   ├── renovate-config.js    # Renovate platform config
│   └── workflows/
│       ├── release.yaml      # Label-driven SemVer release workflow
│       └── renovate.yaml     # Renovate workflow
├── .tool-versions            # Pinned language/tool versions (asdf/mise)
├── AGENTS.md                 # Operational expectations for humans and AI agents
├── CONTRIBUTING.md
├── LICENSE
├── README.md
├── SECURITY.md
└── renovate.json             # Renovate settings
```

## Conventions

- `.tool-versions` is the single source of truth for language and tool versions. Before building, testing, or running any tooling, check it and use the pinned versions (install via `asdf install` or `mise install`). When adding a new language or tool to the project, pin its version there first — never assume a globally installed version.
- Versioning: project artifacts (releases, tags, packages, images) follow [SemVer](https://semver.org/) as bare `X.Y.Z` — no `v` prefix (`1.4.2`, not `v1.4.2`). Bump MAJOR for breaking changes, MINOR for backwards-compatible features, PATCH for fixes.
- See `AGENTS.md` for full agent workflow, code style, testing, and git/PR guidance.
- Branch protection: never push directly to `main`; all changes via PR with review.
- When adapting this template for a new project, update `renovate.json` managers/schedules and enable the repo in the Renovate GitHub App.
- Project-specific `CLAUDE.md` / `AGENTS.md` content should be filled in once the actual stack is added (`src/`, `tests/`, build commands, etc. are placeholders in `AGENTS.md`, not present here).

## Releases

- All released artifacts (images, binaries, packages) are versioned with [SemVer](https://semver.org/) as bare `X.Y.Z` tags — no `v` prefix.
- **Automatic bumps**: when a PR merges to `main`, the next version is derived from the PR label:
  - `major` — breaking changes
  - `minor` — backwards-compatible features
  - `patch` — fixes
  - No label — defaults to a `patch` bump
- **Manual releases**: a specific version may be cut manually by supplying an explicit `X.Y.Z` version tag when needed (e.g. via a manual workflow dispatch). This bypasses the label-based bump.
- Release automation lives in `.github/workflows/release.yaml`: it computes the next version, tags, and creates the GitHub release. Building and publishing artifacts (images, binaries) is project-specific and left as a TODO stub in the workflow.
