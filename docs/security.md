---
title: Security
tags:
  - reference
---

## Choosing a trigger

`ego` never needs the PR's code checked out: it reads the PR through the API. So either trigger works, with different trade-offs.

**`pull_request`**
- PRs from forks get no secrets and a read-only token, so the review fails for them.
- GitHub runs the workflow file *from the PR's branch*. Anyone who can push a branch to your repository can edit the workflow to print your LLM key.

**`pull_request_target`**
- GitHub runs the workflow file from the base branch, so a PR can't change what runs or reach the secrets through it.
- This is safe **only if the job never checks out or runs PR code.** Don't add `actions/checkout` with the PR's head ref, and use `ego` from a pinned release (`ctrl-research/ego@X.Y.Z`), not `./`.
- PRs from forks then get reviewed with your secrets and LLM quota. To skip them, add `if: github.event.pull_request.head.repo.full_name == github.repository`.

**Either way,** anyone with write access can still reach repository and organization secrets through *other* workflows they push. For a key you want locked down, store it as an [environment secret](https://docs.github.com/actions/deployment/targeting-different-environments/using-environments-for-deployment) limited to `main`, rather than as a repository or organization secret. With `pull_request_target`, jobs run on `main`, so they can use it.

`ego`'s own repository reviews its PRs this way: `pull_request_target`, a checkout of the base commit only, and `uses: ./` from that commit.

## PR content is untrusted

- **Prompt injection:** the PR title, description and diff are written by whoever opened the PR, and all of it goes to the model. A malicious PR can try to steer the review, or slip misleading text or links into the comment. The built-in prompt tells the model to treat PR content as data, not instructions, but treat the review as advisory, and keep a human in the loop for merges.
- **Mentions:** model-written text has `@mentions` neutralized (a zero-width space after the `@`), so a PR can't make the bot ping users or teams.
- **`extra-prompt` is trusted:** it's treated as maintainer instructions and ranks above the built-in guidance. Use it only for fixed guidance. Don't build it from CI output (test logs, `terraform plan` output) or anything else a PR can influence.
- **Outputs:** the `review`, `review-json` and `verdict` outputs are model output. In later steps, pass them through `env:` rather than `${{ }}` inside `run:`, which would let that text become shell code.

## Data and credentials

- **Where the diff goes:** the LLM endpoint (`base-url`) receives the full diff. Point it only at an endpoint you trust. To keep code on your own infrastructure, use `provider: openai-compatible` with a self-hosted model.
- **Secrets stay out of the process list:** the LLM key and forge token reach the binary through environment variables, never command-line flags.
- **No redirects:** `ego` never follows HTTP redirects, so neither credential can be forwarded to another host.
- **Download integrity:** release binaries are checked against the release's SHA-256 checksums before they run.
