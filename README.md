# template

Template repository for `ctrl-research` projects.

## What's Included

- **Renovate** — automated dependency updates for Docker, Go modules, and GitHub Actions
- **Releases** — automatic SemVer bumps on merge to `main` via PR labels (`major`/`minor`/`patch`, default `patch`), plus manual dispatch for explicit versions
- **Branch protection** — `main` requires PRs and review
- **CODEOWNERS** — `@ctrl-research/reviewers` auto-requested for review
- **MIT License**
- **.gitignore** — common exclusions for OS, IDE, build outputs, and secrets
- **.tool-versions** — single source of truth for language/tool versions (asdf/mise compatible)

## Using This Template

1. Click **Use this template** to create a new repository
2. Pin your project's language and tool versions in `.tool-versions`
3. Update `renovate.json` to configure managers and schedules for your project
4. Enable the new repo in the Renovate GitHub App if using hosted Renovate
5. Fill in the artifact build/publish TODO stub in `.github/workflows/release.yaml`

## Renovate

Dependency updates are managed via Renovate. Configuration is in `renovate.json` and `.github/renovate-config.js`.

Enabled managers:
- `asdf` (keeps `.tool-versions` up to date)
- `docker-compose`
- `github-actions`
- `gomod`

Add or remove managers as needed for your project.

## Releases

Releases follow [SemVer](https://semver.org/) with bare `X.Y.Z` tags (no `v` prefix).

- **Automatic**: merging a PR to `main` cuts a release based on its `major`, `minor`, or `patch` label; no label means a `patch` bump.
- **Manual**: run the **Release** workflow with an explicit `X.Y.Z` version to cut a specific release.

Automation lives in `.github/workflows/release.yaml` (versioning, tag, GitHub release). Building and publishing artifacts (images, binaries) is project-specific — fill in the TODO stub in the workflow.

## Files

```
.
├── .github/
│   ├── CODEOWNERS           # Auto-request review from @ctrl-research/reviewers
│   ├── renovate-config.js   # Renovate platform config
│   └── workflows/
│       ├── release.yaml     # Label-driven SemVer release workflow
│       └── renovate.yaml    # Renovate GitHub Action workflow
├── .gitignore
├── .tool-versions          # Pinned language/tool versions (asdf/mise)
├── CONTRIBUTING.md
├── LICENSE
├── README.md
├── renovate.json           # Renovate settings
└── SECURITY.md
```
