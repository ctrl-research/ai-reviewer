---
title: Run after other checks
tags:
  - recipe
---

Run `ego` after the other jobs in the same workflow, so its review is the last thing to land on the PR, and fail the run if `ego` marks the change blocking.

- **`needs: [test, lint]`** orders the review after those jobs.
- **`if: ${{ !cancelled() }}`** runs the review even when tests or lint fail, and skips it only if the run was cancelled.
- **The last step** fails the job on a `blocking` verdict. Make the job a required status check to block merging.

`ego` doesn't read the other jobs' results; ordering only controls when the review lands. See [[recipes/index|Recipes]].

<!-- include: examples/run-after-checks.yaml -->

[View on GitHub](https://github.com/ctrl-research/ego/blob/main/examples/run-after-checks.yaml)
