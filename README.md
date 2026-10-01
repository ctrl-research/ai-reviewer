# ego

**The reality check between your agents and `main`.**

In Freud's model of the mind, the *id* acts on impulse, the *superego* holds the rules, and the *ego* weighs the two against reality before anything happens. Agentic development has the same shape: coding agents generate changes on impulse, your conventions sit above them, and `ego` is the step that checks the result before it merges. (It's also named for a certain food critic.)

LLM-powered pull request review. Fetches the PR diff, sends it to a configurable LLM provider, and posts the review as a sticky PR comment (updated in place on subsequent pushes).

Works on **GitHub** (github.com, GitHub Enterprise Server) and **Forgejo/Gitea** Actions runners — both run this composite-action format and expose a GitHub-shaped REST API. Select with the `platform` input.

Supported forges:

| `platform` | API | Token header |
|---|---|---|
| `github` | GitHub REST API (`github.api_url`, e.g. `https://api.github.com`) | `Authorization: Bearer` |
| `forgejo` | Forgejo/Gitea API (e.g. `https://codeberg.org/api/v1`) | `Authorization: token` |

Supported LLM providers:

| `provider` | Endpoint | Auth |
|---|---|---|
| `anthropic` | Anthropic Messages API (`/v1/messages`) | `api-key` required |
| `openai` | OpenAI Chat Completions (`/chat/completions`) | `api-key` required |
| `openai-compatible` | Any OpenAI-compatible server — Ollama, vLLM, LM Studio, OpenRouter, LiteLLM, etc. | `api-key` optional; `base-url` required |

No checkout step is needed — the action reads the diff via the forge REST API.

## Usage — reusable workflow

```yaml
# .github/workflows/pr-review.yaml in your repo
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

### Local/self-hosted LLM (e.g. Ollama)

Point at any OpenAI-compatible endpoint. Use a self-hosted runner label if the endpoint is on a private network:

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

### OpenAI

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

### Forgejo / Gitea

The same composite action runs on a Forgejo Actions runner. Set `platform: forgejo`; `api-url` defaults to the instance API, and the job token reads the PR and posts the comment. Provide the LLM key via an Actions secret:

```yaml
# .forgejo/workflows/pr-review.yaml in your repo
on:
  pull_request:

jobs:
  review:
    runs-on: docker
    steps:
      - name: PR Review
        uses: https://github.com/ctrl-research/ego@main
        with:
          platform: forgejo
          provider: anthropic
          model: claude-opus-4-8
          api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

Pair it with a local LLM (`provider: openai-compatible`, `base-url: ...`) to keep the diff entirely on your own infrastructure.

## Usage — composite action

For more control (custom prompt, consuming the review output in later steps):

```yaml
jobs:
  review:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
    steps:
      - name: PR Review
        id: review
        uses: ctrl-research/ego@main
        with:
          provider: anthropic
          model: claude-opus-4-8
          api-key: ${{ secrets.ANTHROPIC_API_KEY }}
          post-comment: "false"

      - name: Use the review elsewhere
        run: echo "${{ steps.review.outputs.review }}"
```

## Versioning

Releases are tagged with bare SemVer (`X.Y.Z`, no `v` prefix). Pin to a release tag (e.g. `ctrl-research/ego@1.0.0`) instead of `@main` for reproducible reviews. Note that the reusable workflow always calls the composite action at `@main` (built from source on every run); use the composite action directly if you need the whole pipeline pinned or want the prebuilt binary.

Merging a PR to `main` cuts a release from its `major`/`minor`/`patch` label (default `patch`); the **Release** workflow can also be dispatched manually with an explicit version.

## Running locally

The same binary works as a CLI. Non-secret settings are flags (see `ego --help`); secrets come from the environment:

```bash
go build -o ego ./cmd/ego
EGO_TOKEN="$(gh auth token)" EGO_API_KEY="$ANTHROPIC_API_KEY" \
  ./ego --repo ctrl-research/ego --pr 42 --post-comment=false
```

With `--post-comment=false` and no `GITHUB_OUTPUT` set, the review is printed to stdout.

## Inputs

| Input | Default | Description |
|---|---|---|
| `platform` | `github` | Forge hosting the PR: `github` or `forgejo` (also covers Gitea) |
| `api-url` | `github.api_url` | Forge REST API base URL. Override for self-hosted instances |
| `provider` | `anthropic` | `anthropic`, `openai`, or `openai-compatible` |
| `base-url` | provider default | LLM API base URL. Required for `openai-compatible` |
| `model` | `claude-opus-4-8` | Model ID |
| `api-key` | — | LLM provider API key (pass from a secret) |
| `github-token` | `github.token` | Forge token for reading the diff and posting the comment |
| `pr-number` | from event | PR number when not running on a `pull_request` event |
| `max-tokens` | `16000` | Max output tokens |
| `max-diff-bytes` | `300000` | Diff truncation limit |
| `review-prompt` | built-in | Override the review system prompt |
| `post-comment` | `true` | Post/update the sticky PR comment |

## Outputs

| Output | Description |
|---|---|
| `review` | The generated review body (markdown) |

## Notes

- The review runs in a self-contained Go binary (`cmd/ego`); the only runner requirement is `bash` and `curl` to download it. Release refs (`@X.Y.Z`) download a prebuilt binary from the GitHub release and verify its SHA-256 checksum. Any other ref (`@main`, a commit SHA) builds from source with `actions/setup-go`, which adds roughly 10–30s and puts that Go version on `PATH` for later steps in the same job — run the review in its own job, or pin a release, if that matters.
- Failed requests to the forge (reads only) and the LLM are retried twice on connection errors and HTTP 408/409/429/5xx, honoring `Retry-After`.
- If the model stops at `max-tokens`, the review is still posted and a warning is logged. If the model declines the request (Anthropic `refusal` stop reason or OpenAI `refusal`), the step fails.
- The sticky comment is identified by an HTML marker (`<!-- pr-review-action -->`); re-runs update it instead of stacking new comments.
- Diffs larger than `max-diff-bytes` are truncated with a notice appended, so the model knows the diff is partial.
- The `openai`/`openai-compatible` path sends `max_tokens`; some newer OpenAI models require `max_completion_tokens` instead — prefer broadly-compatible models or a proxy (LiteLLM) if you hit that.

## Security

- **Trigger with `pull_request`, not `pull_request_target`.** On `pull_request`, PRs from forks get a read-only token and no secrets — so for fork PRs this action simply no-ops (it cannot post). That is the safe default. `pull_request_target` runs with your secrets and a write token in the context of an untrusted PR; do not use it here.
- **The PR title, body, and diff are untrusted, attacker-controlled input** that is fed to the LLM. The review comment is therefore attacker-influenceable: a malicious PR can attempt prompt injection to skew the review or embed misleading links. The system prompt instructs the model to treat PR content as data, but treat the generated review as advisory, not authoritative. Keep a human in the loop for merge decisions.
- The LLM endpoint (`base-url`) receives the full diff. Point it only at an endpoint you trust; use `provider: openai-compatible` with a self-hosted model to keep code on your own infrastructure.
- Secrets (LLM API key, forge token) reach the binary through environment variables, never command-line flags, so they do not appear in the runner's process list. Redirects are never followed, so neither secret can be forwarded to another host.
- Anyone can post a PR comment that starts with the sticky marker. If the matching comment can't be edited with the job token, the action posts a new comment instead of failing.
