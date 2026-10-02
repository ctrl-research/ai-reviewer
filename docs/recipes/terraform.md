---
title: Terraform
tags:
  - recipe
---

Format, validate and plan first, then have `ego` review the HCL changes with a Terraform-specific checklist, and block merges `ego` marks blocking.

The checklist in `extra-prompt` asks the model to look for:

- **Destroys and replacements:** renamed resources or modules without a `moved` block, changed immutable arguments, and `count`/`for_each` key changes. Replacing a data store (database, bucket, volume) is treated as blocking unless the PR explains it.
- **Weakened protection:** removed `prevent_destroy`, `deletion_protection` or backups.
- **IAM:** wildcard actions or resources, and new admin or cross-account access.
- **Network exposure:** `0.0.0.0/0` or `::/0` ingress, public buckets or snapshots, and new public IPs.
- **Secrets:** hard-coded credentials, defaults on secret variables, and secrets without `sensitive = true`.
- **Encryption** turned off or missing, **loosened version constraints**, **backend or state changes**, and **missing tags**.

Adjust the list to your own conventions. It's fixed guidance written by you, which is what `extra-prompt` is for.

**What this doesn't do:** `ego` reviews the diff, not the plan output. Keep posting the plan with the tool you already use (for example tfcmt, Atlantis or Terraform Cloud). Don't paste plan output into `extra-prompt`: plan output contains values a PR can control, and `extra-prompt` is treated as trusted instructions.

**Credentials:** the plan job needs your cloud credentials. On `pull_request`, they're as exposed as any other secret to people who can push branches; see [[security|Security]].

<!-- include: examples/terraform.yaml -->

[View on GitHub](https://github.com/ctrl-research/ego/blob/main/examples/terraform.yaml)
