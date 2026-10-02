# ego

> *"Do you know what I'm craving? A little perspective."*
> — Anton Ego, *Ratatouille*

`ego` is that perspective for your pull requests: an outside reviewer that reads the diff before it merges.

The name also borrows from Freud. In his model of the mind, the *id* acts on impulse, the *superego* holds the rules, and the *ego* weighs the two against reality before anything happens. Agentic development has the same shape: coding agents generate changes on impulse, your conventions sit above them, and `ego` is the reality check before anything reaches `main`.

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

## The review

The model returns a structured review (JSON). `ego` turns it into one sticky comment, which it updates in place on every push:

| Section | Contents |
|---|---|
| **Verdict** | ✅ Looks good / ⚠️ Needs changes / 🛑 Blocking, a risk level, and a one-line reason |
| **Summary** | What the PR changes |
| **Code review** | Findings ranked by severity (🛑 critical, 🔴 high, 🟠 medium, 🟡 low), each with file, line, problem and fix |
| **Dependency changes** | Version bumps in the diff, with breaking changes taken from release notes in the PR description (e.g. Renovate's). Shown only when versions change |
| **Security review** | The change's security impact, plus any security findings |
| **Tests** | Whether the change is covered by tests, and the gaps |
| **Questions for the author** | Things the model couldn't determine from the diff |
| **Reviewer info** | Collapsed: provider, model, model version reported by the API, ego version, tokens, estimated cost |

Example (concise mode):

```markdown
## ⚠️ Needs changes · Risk: **medium**

The new cache is read and written from two goroutines without a lock.

### Summary

Adds an in-memory cache in front of the user lookup.

### Code review

- 🔴 **High** · Data race on cache map · `cache/cache.go:41`

  `Get` and `Set` access the map from request goroutines with no synchronization.

  **Fix:** Guard the map with a `sync.RWMutex`.

<details><summary>1 medium/low finding</summary> … </details>

### Security review

No security impact: the cache holds only public profile fields.

### Tests

**Coverage:** ⚠️ Partial · Unit tests cover hits and misses.

- Concurrent access test (`go test -race`)

---
<details><summary><sub>Reviewed by ego 0.1.0 · claude-opus-4-8 · 18.2k in / 1.1k out · ≈ $0.12</sub></summary> … </details>
```

**Tuning it**
- `concise: true` (default) asks for shorter text and folds medium/low findings and questions into collapsed sections. `concise: false` asks for a thorough review with everything expanded.
- `reviewer-info: false` drops the reviewer-info block.
- `extra-prompt` appends your own guidance to the built-in prompt and keeps the structured format, e.g. `extra-prompt: "We use sqlc; flag hand-written SQL. Public API changes need a CHANGELOG entry."`
- `review-prompt` replaces the built-in prompt entirely. The model's output is then posted as-is (markdown), without the sections above, and the `review-json`/`verdict` outputs are empty. `extra-prompt` is still appended, and reviewer info is still added.

**Structured output.** On Anthropic models that support it (Fable 5/5.1, Opus 5.5/5/4.8, Sonnet 5.5/5, Haiku 4.5), `ego` enforces the JSON format natively. Other models get the schema in the prompt, and `ego` extracts the JSON from the response, including from code fences or surrounding prose. If no valid review is found, it posts the model's raw output under a warning instead of failing.

**Cost** is an estimate. Without `input-price`/`output-price`, `ego` uses built-in Anthropic list prices (ignoring caching and batch discounts). For other providers it shows token counts only, since a guessed price would be worse than none.

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
| `review-prompt` | built-in | Replace the built-in prompt; output is posted as-is (see [The review](#the-review)) |
| `extra-prompt` | — | Extra instructions appended to the reviewer prompt |
| `concise` | `true` | Prefer concise reviews; fold medium/low findings and questions |
| `reviewer-info` | `true` | Add the collapsed reviewer-info block |
| `input-price` | built-in / — | USD per million input tokens for the cost estimate (set with `output-price`) |
| `output-price` | built-in / — | USD per million output tokens for the cost estimate (set with `input-price`) |
| `extra-body` | — | JSON object merged into the LLM request body; `null` removes a key (see Notes) |
| `post-comment` | `true` | Post/update the sticky PR comment |

## Outputs

| Output | Description |
|---|---|
| `review` | The rendered review (markdown), as posted without the sticky marker |
| `review-json` | The structured review as JSON (empty with a custom `review-prompt` or unparseable output) |
| `verdict` | `approve`, `needs_changes` or `blocking` (empty when `review-json` is) |

For example, to fail the job on a blocking review:

```yaml
      - name: Fail on blocking review
        if: steps.review.outputs.verdict == 'blocking'
        run: exit 1
```

## Notes

- The review runs in a self-contained Go binary (`cmd/ego`); the only runner requirement is `bash` and `curl` to download it. Release refs (`@X.Y.Z`) download a prebuilt binary from the GitHub release and verify its SHA-256 checksum. Any other ref (`@main`, a commit SHA) builds from source with `actions/setup-go`, which adds roughly 10–30s and puts that Go version on `PATH` for later steps in the same job — run the review in its own job, or pin a release, if that matters.
- Failed requests to the forge (reads only) and the LLM are retried twice on connection errors and HTTP 408/409/429/5xx, honoring `Retry-After`.
- If the model stops at `max-tokens`, the review is still posted and a warning is logged. If the model declines the request (Anthropic `refusal` stop reason or OpenAI `refusal`), the step fails.
- The sticky comment is identified by an HTML marker (`<!-- pr-review-action -->`); re-runs update it instead of stacking new comments.
- Model-written text has `@mentions` neutralized (a zero-width space after the `@`), so a prompt-injected diff can't make the bot ping users or teams. Comments are capped just under GitHub's 65,536-character limit.
- Diffs larger than `max-diff-bytes` are truncated with a notice appended, so the model knows the diff is partial.
- `extra-body` passes provider-specific parameters. It can't set `model`, `messages` or `system`; use the matching inputs. Examples:
  - MiniMax: `'{"reasoning_split": true}'` returns reasoning in a separate `reasoning_content` field instead of inline `<think>` tags.
  - Newer OpenAI models that reject `max_tokens`: `'{"max_tokens": null, "max_completion_tokens": 16000}'`.
- On the `openai`/`openai-compatible` path, a leading `<think>…</think>` block in the response (how MiniMax M2.x/M3, DeepSeek-R1 and Qwen via Ollama return reasoning inline) is stripped at the first `</think>`. That breaks if the reasoning itself quotes `</think>`, so prefer an out-of-band option such as MiniMax's `reasoning_split` where the server offers one. Reasoning counts toward `max-tokens`; if the model runs out mid-thought, the step fails and asks you to raise it.

## Security

- **Choose the trigger with your secrets in mind.** `ego` never needs the PR's code checked out: it reads the diff through the API. That makes either trigger workable, with different trade-offs:
  - **`pull_request`**: fork PRs get no secrets and a read-only token, so the review simply fails for them. But GitHub runs the workflow file *from the PR branch*, so anyone who can push a branch to your repo can edit the workflow to print your LLM key.
  - **`pull_request_target`**: GitHub runs the workflow file from the base branch, so a PR can't change what runs or reach the secrets through it. This is safe **only if the job never checks out or executes PR code**. Don't add `actions/checkout` with the PR head ref, and use `ego` from a pinned release (`ctrl-research/ego@X.Y.Z`), not `./`. Fork PRs then get reviewed with your secrets and LLM quota; skip them with `if: github.event.pull_request.head.repo.full_name == github.repository` if you don't want that.
  - Either way, anyone with write access can still reach repo and org secrets through *other* workflows they push. For a key you want locked down, store it as an [environment](https://docs.github.com/actions/deployment/targeting-different-environments/using-environments-for-deployment) secret limited to `main` (with `pull_request_target`, jobs run on `main`), rather than as a repo or org secret.
- **The PR title, body, and diff are untrusted, attacker-controlled input** that is fed to the LLM. The review comment is therefore attacker-influenceable: a malicious PR can attempt prompt injection to skew the review or embed misleading links. The system prompt instructs the model to treat PR content as data, but treat the generated review as advisory, not authoritative. Keep a human in the loop for merge decisions.
- The LLM endpoint (`base-url`) receives the full diff. Point it only at an endpoint you trust; use `provider: openai-compatible` with a self-hosted model to keep code on your own infrastructure.
- Secrets (LLM API key, forge token) reach the binary through environment variables, never command-line flags, so they do not appear in the runner's process list. Redirects are never followed, so neither secret can be forwarded to another host.
- Anyone can post a PR comment that starts with the sticky marker. If the matching comment can't be edited with the job token, the action posts a new comment instead of failing.
