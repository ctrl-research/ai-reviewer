---
title: The review
tags:
  - guide
---

The model returns a structured review (JSON), and `ego` renders it into one sticky comment on the PR. Each push updates that comment in place, so the PR never collects a stack of old reviews.

## Sections

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

Code review and Security review always appear, and say so when there's nothing to report. Dependency changes, Tests and Questions appear only when they have content.

## Example

This is a review in concise mode, the default:

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

## Tuning the review

- **`concise`** (default `true`) asks for shorter text, and folds medium/low findings and questions into collapsed sections. Set `concise: false` for a thorough review with everything expanded and lower-severity improvements included.
- **`reviewer-info`** (default `true`) adds the collapsed block with provider, model, model version, `ego` version, tokens and estimated cost. Set it to `false` to drop the block.
- **`extra-prompt`** appends your own guidance to the built-in prompt and keeps the structured format. It takes priority over the built-in guidance but can't change the format. For example:

  ```yaml
  extra-prompt: |
    We use sqlc; flag hand-written SQL.
    Public API changes need a CHANGELOG entry.
  ```

  Use it for fixed guidance only. Don't put CI output or other PR-influenced text in it; see [[security|Security]].
- **`review-prompt`** replaces the built-in prompt entirely. The model's output is then posted as-is (markdown), without the sections above, and the `review-json` and `verdict` outputs are empty. `extra-prompt` is still appended, and reviewer info is still added.

## Gating merges on the verdict

The `verdict` output is `approve`, `needs_changes` or `blocking`. To fail the job on a blocking review:

```yaml
      - name: Review with ego
        id: ego
        uses: ctrl-research/ego@main
        with:
          api-key: ${{ secrets.ANTHROPIC_API_KEY }}

      - name: Fail on a blocking review
        if: steps.ego.outputs.verdict == 'blocking'
        run: |
          echo "::error::ego marked this change as blocking. See the review comment on the PR."
          exit 1
```

Make that job a required status check to block merging. Treat the verdict as a strong signal, not a substitute for a human reviewer: a PR can try to steer the model (see [[security|Security]]).

## How the JSON is obtained

- **Native structured output:** on Anthropic models that support it (Fable 5/5.1, Opus 5.5/5/4.8, Sonnet 5.5/5, Haiku 4.5), the API enforces the JSON format.
- **Everything else:** other models get the schema in the prompt, and `ego` extracts the JSON from the response, whether it's bare, in a code fence, or surrounded by prose. Unknown values are normalized.
- **Fallback:** if no valid review is found, `ego` posts the model's raw output under a warning instead of failing the job.

## Cost

The reviewer-info block shows input and output tokens, and an estimated cost when a price is known:

- **Built-in prices:** Anthropic list prices, ignoring prompt-caching and batch discounts.
- **Your own prices:** set `input-price` and `output-price` (USD per million tokens) for any other provider, or to override the built-in ones.
- **Otherwise,** only token counts are shown. A guessed price would be worse than none.
