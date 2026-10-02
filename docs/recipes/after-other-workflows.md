---
title: After other workflows
tags:
  - recipe
---

When your checks live in a separate workflow file, start `ego` with `workflow_run` when that workflow completes.

- **One review per listed workflow:** `workflow_run` fires once for each listed workflow that completes. List one workflow (usually the slowest), or the PR is reviewed once per workflow.
- **Secrets:** the run uses this file as it is on the default branch and has access to secrets, like `pull_request_target`. That's safe here because `ego` never runs PR code. Don't add a checkout of the PR's head to this workflow.
- **Fork PRs:** GitHub doesn't attach PRs from forks to `workflow_run` events, so those are skipped.
- **The PR number** comes from the event, so it's passed explicitly with `pr-number`.

<!-- include: examples/after-other-workflows.yaml -->

[View on GitHub](https://github.com/ctrl-research/ego/blob/main/examples/after-other-workflows.yaml)
