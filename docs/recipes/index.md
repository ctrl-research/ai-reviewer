---
title: Recipes
tags:
  - recipe
---

Complete workflows for common setups. Each one is a real file in [`examples/`](https://github.com/ctrl-research/ego/tree/main/examples), linted in CI, so you can copy it as-is.

| Recipe | When to use it |
|---|---|
| [Run after other checks](run-after-checks) | Your tests and linters are in the same workflow, and you want the review to land last and gate merges on its verdict |
| [Terraform](terraform) | Terraform changes: `fmt`, `validate` and `plan` first, then a review with a Terraform-specific checklist |
| [After other workflows](after-other-workflows) | Your checks are in a separate workflow file |
| [Your own bot identity](app-identity) | Post reviews as your own GitHub App instead of `github-actions[bot]` |

## What ego sees

`ego` reviews the PR's title, description and diff. It doesn't read other checks' results, job logs or PR comments, such as a posted `terraform plan`. Running it after your checks makes its review the last word on the PR and lets you gate on its `verdict`, but the model doesn't see those checks' output.

Don't paste CI output into `extra-prompt` to work around this. That input is treated as trusted maintainer instructions, and test logs or plan output contain values a PR can control. See [[security|Security]].

## Rolling it out across an organization

Add a workflow to each repository. On GitHub Enterprise Cloud or Enterprise Server, you can instead require a workflow from an organization ruleset ("require workflows to pass before merging"), which runs a workflow from a central repository on every targeted repository without per-repository files.
