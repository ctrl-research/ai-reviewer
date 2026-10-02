---
title: ego
tags:
  - landing
---

> *"Do you know what I'm craving? A little perspective."*
> — Anton Ego, *Ratatouille*

`ego` is that perspective for your pull requests: an outside reviewer that reads the diff before it merges. It's a GitHub Action (it also runs on Forgejo and Gitea) that sends the PR to the LLM of your choice and posts one structured review comment, updated in place on every push.

The name also borrows from Freud. In his model of the mind, the *id* acts on impulse, the *superego* holds the rules, and the *ego* weighs the two against reality before anything happens. Agentic development has the same shape: coding agents generate changes on impulse, your conventions sit above them, and `ego` is the reality check before anything reaches `main`.

## What you get

- **A verdict:** ✅ Looks good, ⚠️ Needs changes or 🛑 Blocking, with a risk level and a one-line reason.
- **Code and security findings**, ranked by severity, each with file, line and a concrete fix.
- **Dependency, test and open-question sections**, shown when they apply.
- **Reviewer info:** model, versions, tokens and an estimated cost.
- **Any provider:** Anthropic, OpenAI, or any OpenAI-compatible server (Ollama, vLLM, MiniMax, OpenRouter and others).
- **No checkout and no app:** `ego` reads the PR through the API and comments with the job's own token.

## Quick start

Add `.github/workflows/pr-review.yaml` to a repository and an `ANTHROPIC_API_KEY` secret:

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
    secrets:
      llm-api-key: ${{ secrets.ANTHROPIC_API_KEY }}
```

Open a pull request, and the review appears as a comment.

## Where next

- [[getting-started|Getting started]]: setup, other providers, and Forgejo.
- [[review|The review]]: what each section means, and how to tune it.
- [[configuration|Configuration]]: every input and output.
- [[recipes/index|Recipes]]: running after other checks, Terraform, `workflow_run`, a custom bot identity.
- [[providers|Providers and forges]]: provider-specific notes.
- [[security|Security]]: triggers, secrets and untrusted input.
- [[cli|Running locally]]: the same binary as a CLI.
- [[development|Releases and development]]: versioning, how releases work, and the codebase.
