---
title: Getting started
tags:
  - guide
---

## What a repository needs

`ego` is a GitHub Action, not a GitHub App, so there's nothing to install on the organization. It runs in each repository's own workflow and uses that job's `GITHUB_TOKEN`, so reviews are posted as `github-actions[bot]`.

- **A workflow** that calls `ego`, with `pull-requests: write` in its `permissions:`. Declaring it works even when the organization's default token is read-only.
- **An LLM key**, as a repository secret or an organization secret shared with the repository. Self-hosted models may not need one.
- **Actions enabled** for the repository. If the organization allows only selected actions, add `ctrl-research/ego@*`, plus `actions/setup-go@*` if you use `ego` from a branch or commit instead of a release, since those build from source.

No `actions/checkout` step is needed: `ego` reads the PR's title, description and diff through the forge's API.

## Two ways to use it

**The reusable workflow** is the shortest setup. It runs `ego` in its own job:

```yaml
name: PR Review
on:
  pull_request:

permissions:
  contents: read
  pull-requests: write

jobs:
  review:
    uses: ctrl-research/ego/.github/workflows/pr-review.yaml@main
    with:
      provider: anthropic
      model: claude-opus-4-8
    secrets:
      llm-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

**The composite action** is a step in your own job. Use it to pin a release (and get the prebuilt binary), to run after other jobs, or to use the review's outputs in later steps:

```yaml
jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - name: Review with ego
        id: ego
        uses: ctrl-research/ego@main # or a release, e.g. @X.Y.Z
        with:
          api-key: ${{ secrets.ANTHROPIC_API_KEY }}

      - name: Use the review in a later step
        env:
          # Pass outputs through env, never ${{ }} inside run: the review is
          # model output that a PR can influence.
          REVIEW: ${{ steps.ego.outputs.review }}
          VERDICT: ${{ steps.ego.outputs.verdict }}
        run: |
          echo "Verdict: $VERDICT"
          printf '%s\n' "$REVIEW" >> "$GITHUB_STEP_SUMMARY"
```

The reusable workflow always calls the action at `@main`, which builds from source on every run (roughly 10–30 seconds). Use the composite action at a release tag to download the prebuilt binary instead. See [[development|Releases and development]].

## Other providers

**OpenAI:**

```yaml
jobs:
  review:
    uses: ctrl-research/ego/.github/workflows/pr-review.yaml@main
    with:
      provider: openai
      model: gpt-4o
    secrets:
      llm-api-key: ${{ secrets.OPENAI_API_KEY }}
```

**A self-hosted model (e.g. Ollama).** Any OpenAI-compatible server works. Use a self-hosted runner if the endpoint is on a private network:

```yaml
jobs:
  review:
    uses: ctrl-research/ego/.github/workflows/pr-review.yaml@main
    with:
      provider: openai-compatible
      base-url: http://ollama.internal:11434/v1
      model: llama3.1:70b
      runs-on: self-hosted
```

**MiniMax:**

```yaml
      - name: Review with ego
        uses: ctrl-research/ego@main
        with:
          provider: openai-compatible
          base-url: https://api.minimax.io/v1
          api-key: ${{ secrets.MINIMAX_API_TOKEN }}
          model: MiniMax-M3
          # Return reasoning separately instead of inline <think> tags.
          extra-body: '{"reasoning_split": true}'
```

See [[providers|Providers and forges]] for provider-specific notes.

## Forgejo and Gitea

The same composite action runs on Forgejo Actions runners. Set `platform: forgejo`. `api-url` defaults to the instance's API, and the job token reads the PR and posts the comment:

```yaml
# .forgejo/workflows/pr-review.yaml
on:
  pull_request:

jobs:
  review:
    runs-on: docker
    steps:
      - name: Review with ego
        uses: https://github.com/ctrl-research/ego@main
        with:
          platform: forgejo
          provider: anthropic
          model: claude-opus-4-8
          api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

Pair it with a self-hosted model (`provider: openai-compatible`) to keep the diff entirely on your own infrastructure.

## Next

- [[review|The review]]: what the comment contains, and how to tune it.
- [[security|Security]]: read before choosing between `pull_request` and `pull_request_target`.
- [[recipes/index|Recipes]]: running after other checks, Terraform, and more.
