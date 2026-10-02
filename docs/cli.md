---
title: Running locally
tags:
  - guide
---

The binary behind the action also works as a command-line tool. It's useful for trying a prompt or model on a real PR before changing a workflow.

## Build

With the Go version pinned in the repository's `.tool-versions`:

```bash
go build -o ego ./cmd/ego
```

Or download a prebuilt binary (`ego_<os>_<arch>`) from a [release](https://github.com/ctrl-research/ego/releases).

## Run

Non-secret settings are flags. Secrets come from the environment only, so they never appear in the process list:

```bash
EGO_TOKEN="$(gh auth token)" EGO_API_KEY="$ANTHROPIC_API_KEY" \
  ./ego --repo ctrl-research/ego --pr 42 --post-comment=false
```

With `--post-comment=false` and no `GITHUB_OUTPUT` set, the rendered review is printed to stdout.

## Settings

Every action input has a matching flag and an `EGO_*` environment variable. Flags take precedence:

| Flag | Environment variable |
|---|---|
| `--platform` | `EGO_PLATFORM` |
| `--api-url` | `EGO_API_URL` (falls back to `GITHUB_API_URL`) |
| `--repo` | `EGO_REPO` (falls back to `GITHUB_REPOSITORY`) |
| `--pr` | `EGO_PR_NUMBER` |
| `--provider`, `--base-url`, `--model` | `EGO_PROVIDER`, `EGO_BASE_URL`, `EGO_MODEL` |
| `--max-tokens`, `--max-diff-bytes` | `EGO_MAX_TOKENS`, `EGO_MAX_DIFF_BYTES` |
| `--concise`, `--reviewer-info` | `EGO_CONCISE`, `EGO_REVIEWER_INFO` |
| `--input-price`, `--output-price` | `EGO_INPUT_PRICE`, `EGO_OUTPUT_PRICE` |
| `--extra-body` | `EGO_EXTRA_BODY` |
| `--post-comment` | `EGO_POST_COMMENT` |
| (environment only) | `EGO_TOKEN` (falls back to `GITHUB_TOKEN`), `EGO_API_KEY` |
| (environment only) | `EGO_REVIEW_PROMPT`, `EGO_EXTRA_PROMPT` |

Run `ego --help` for the full list, and `ego --version` for the build version.
