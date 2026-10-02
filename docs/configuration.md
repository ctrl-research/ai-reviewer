---
title: Configuration
tags:
  - reference
---

## Inputs

Inputs of the composite action (`uses: ctrl-research/ego@<ref>`). Booleans are the strings `"true"` or `"false"`.

| Input | Default | Description |
|---|---|---|
| `platform` | `github` | Forge hosting the PR: `github` or `forgejo` (also covers Gitea) |
| `api-url` | `github.api_url` | Forge REST API base URL. Override for self-hosted instances, e.g. `https://codeberg.org/api/v1` |
| `github-token` | `github.token` | Forge token for reading the PR and posting the comment |
| `pr-number` | from the event | PR number, when not running on a `pull_request` event (e.g. `workflow_run`) |
| `provider` | `anthropic` | `anthropic`, `openai` or `openai-compatible` |
| `base-url` | provider default | LLM API base URL. Required for `openai-compatible` |
| `model` | `claude-opus-4-8` | Model ID |
| `api-key` | — | LLM provider API key. Pass it from a secret |
| `max-tokens` | `16000` | Maximum output tokens. Reasoning models count their reasoning toward this |
| `max-diff-bytes` | `300000` | The diff is truncated beyond this many bytes, with a note so the model knows it's partial |
| `concise` | `true` | Prefer concise reviews; fold medium/low findings and questions. See [The review](review) |
| `reviewer-info` | `true` | Add the collapsed reviewer-info block |
| `extra-prompt` | — | Extra instructions appended to the built-in prompt. Keeps the structured format |
| `review-prompt` | built-in | Replace the built-in prompt entirely; the output is posted as-is |
| `input-price` | built-in / — | USD per million input tokens, for the cost estimate. Set together with `output-price` |
| `output-price` | built-in / — | USD per million output tokens, for the cost estimate. Set together with `input-price` |
| `extra-body` | — | JSON object merged into the LLM request body. See below |
| `post-comment` | `true` | Post or update the sticky PR comment. Set `false` to only produce outputs |

## Outputs

| Output | Description |
|---|---|
| `review` | The rendered review (markdown), as posted, without the sticky-comment marker |
| `review-json` | The structured review as JSON. Empty with a custom `review-prompt`, or if the response couldn't be parsed |
| `verdict` | `approve`, `needs_changes` or `blocking`. Empty when `review-json` is |

Outputs contain model-written text that a PR can influence. In later steps, pass them through `env:` rather than `${{ }}` inside `run:`, which would let that text become shell code.

## The reusable workflow

`ctrl-research/ego/.github/workflows/pr-review.yaml` accepts the same inputs, except `github-token` and `pr-number` (it uses the job token and the triggering PR), plus:

| Input | Default | Description |
|---|---|---|
| `runs-on` | `ubuntu-latest` | Runner label, e.g. a self-hosted runner that can reach a private model |

The API key is a secret rather than an input:

```yaml
    secrets:
      llm-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

In the reusable workflow, `max-tokens` and `max-diff-bytes` are numbers, and `concise`, `reviewer-info` and `post-comment` are booleans.

## extra-body

`extra-body` passes provider-specific parameters. It's a JSON object merged into the request body:

- Objects merge key by key, so you can set a nested field without replacing its siblings.
- A `null` value removes a key.
- It can't set `model`, `messages` or `system`; use the matching inputs instead.

Examples:

| Need | `extra-body` |
|---|---|
| MiniMax: return reasoning separately instead of inline `<think>` tags | `'{"reasoning_split": true}'` |
| Newer OpenAI models that reject `max_tokens` | `'{"max_tokens": null, "max_completion_tokens": 16000}'` |
| Anthropic: raise effort (keeps `ego`'s structured-output format) | `'{"output_config": {"effort": "high"}}'` |

## Behavior worth knowing

- **One comment per PR:** the sticky comment is found by an HTML marker (`<!-- pr-review-action -->`), and re-runs update it instead of adding new comments. If the comment with the marker can't be edited with the job's token (for example, someone else posted it), `ego` posts a new one instead of failing.
- **Retries:** forge reads and LLM requests are retried twice on connection errors and HTTP 408, 409, 429 and 5xx, honoring `Retry-After`. Posting the comment isn't retried.
- **Running out of tokens:** if the model stops at `max-tokens`, the review is still posted and a warning is logged.
- **Refusals:** if the model declines to review (Anthropic's `refusal` stop reason, or an OpenAI `refusal`), the step fails.
- **Comment size:** comments are capped just under GitHub's 65,536-character limit.
