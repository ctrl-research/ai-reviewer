---
title: Your own bot identity
tags:
  - recipe
---

Post reviews as your own GitHub App (for example `ego[bot]` with its own avatar) instead of `github-actions[bot]`. No server is involved: the workflow mints a short-lived app token on each run with [`actions/create-github-app-token`](https://github.com/actions/create-github-app-token).

## Setup

1. Create a GitHub App with these repository permissions: **Pull requests: Read and write**, and **Contents: Read**.
2. Install it on the repositories to review.
3. Store its client ID as the `EGO_APP_CLIENT_ID` Actions variable, and its private key as the `EGO_APP_PRIVATE_KEY` secret.

Existing reviews posted by `github-actions[bot]` can't be edited by the app, so the first run posts a new comment; later runs update that one.

<!-- include: examples/app-identity.yaml -->

[View on GitHub](https://github.com/ctrl-research/ego/blob/main/examples/app-identity.yaml)
