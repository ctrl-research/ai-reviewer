---
title: Providers and forges
tags:
  - reference
---

## LLM providers

| `provider` | Endpoint | Auth |
|---|---|---|
| `anthropic` | Anthropic Messages API (`/v1/messages`) | `api-key` required |
| `openai` | OpenAI Chat Completions (`/chat/completions`) | `api-key` required |
| `openai-compatible` | Any OpenAI-compatible server: Ollama, vLLM, LM Studio, MiniMax, OpenRouter, LiteLLM and others | `base-url` required; `api-key` optional |

`base-url` defaults to `https://api.anthropic.com` for `anthropic` and `https://api.openai.com/v1` for `openai`. Point it at a proxy or gateway if you use one.

### Anthropic

- The default model is `claude-opus-4-8`.
- On models that support structured outputs (Fable 5/5.1, Opus 5.5/5/4.8, Sonnet 5.5/5, Haiku 4.5), the API enforces the review's JSON format. Opus 4.7/4.6 and Sonnet 4.6 don't support it, so they get the format through the prompt.
- Cost estimates use built-in list prices.
- To raise effort, use `extra-body: '{"output_config": {"effort": "high"}}'`. It merges with the format `ego` sets.

### OpenAI

- Newer models reject `max_tokens` in favor of `max_completion_tokens`. Swap them with `extra-body: '{"max_tokens": null, "max_completion_tokens": 16000}'`.
- Set `input-price` and `output-price` to get a cost estimate.

### MiniMax

- Use `provider: openai-compatible` with `base-url: https://api.minimax.io/v1`.
- MiniMax models return their reasoning inline as `<think>…</think>` unless asked otherwise. Pass `extra-body: '{"reasoning_split": true}'` to get it in a separate field instead.
- If the review itself mentions a literal `<think>` tag, MiniMax may cut its answer at that point. That mostly affects reviews of code that handles those tags.

### Ollama, vLLM and other self-hosted servers

- Use `provider: openai-compatible` with the server's OpenAI-compatible base URL (for Ollama, `http://<host>:11434/v1`).
- If the server is on a private network, run the job on a self-hosted runner that can reach it (`runs-on` in the reusable workflow).
- Some models (DeepSeek-R1, Qwen and others) return reasoning inline as a leading `<think>…</think>` block. `ego` strips it, splitting at the first `</think>`. That split goes wrong if the reasoning itself quotes `</think>`, so prefer a server option that returns reasoning separately when there is one.

### Reasoning and max-tokens

Reasoning counts toward `max-tokens`. If a model runs out while still reasoning, the step fails and asks you to raise `max-tokens`. If it runs out while writing the review, the review is posted with a warning.

## Forges

| `platform` | API | Token header |
|---|---|---|
| `github` | GitHub REST API (`github.api_url`, e.g. `https://api.github.com`, or GitHub Enterprise Server) | `Authorization: Bearer` |
| `forgejo` | Forgejo or Gitea API (e.g. `https://codeberg.org/api/v1`) | `Authorization: token` |

Both run the same composite action and expose a GitHub-shaped REST API. On Forgejo, `api-url` defaults to the running instance's API, and the job token reads the PR and posts the comment.

## Runner requirements

The review runs in a self-contained Go binary, so the runner needs only `bash` and `curl`:

- **Release refs** (`@X.Y.Z`) download a prebuilt binary for the runner's OS and architecture (Linux, macOS or Windows, on amd64 or arm64) and verify its SHA-256 checksum.
- **Any other ref** (`@main`, a commit SHA) builds from source with `actions/setup-go`. That adds roughly 10–30 seconds, and leaves that Go version on `PATH` for later steps in the same job. Run the review in its own job, or pin a release, if that matters.
