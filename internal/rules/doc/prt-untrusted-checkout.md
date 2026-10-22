# prt-untrusted-checkout

## What it checks

On `pull_request_target`, `issue_comment`, `workflow_run` or `discussion_comment`, a checkout whose `ref:` resolves to
the pull request head (`github.event.pull_request.head.sha`/`.head.ref`, `github.head_ref`,
`github.event.workflow_run.head_sha`/`.head_branch`).

## Why it matters

Those triggers run with secrets and a write token in the base context; checking out attacker code exposes both.

## Example: flagged

```yaml
name: Example
on: pull_request_target
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    permissions: read-all
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.event.pull_request.head.sha }}
      - run: ls -la
```

## Example: passes

```yaml
name: Example
on: pull_request_target
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: ls -la
```

## When it does not fire

Base-ref checkouts, the unprivileged `pull_request` trigger, and refs not in the untrusted set.

## Suppress

`# jobsworth-ignore prt-untrusted-checkout` on the line, or
`rules: { prt-untrusted-checkout: off }` in `.jobsworth.yml`.

## Related

`prt-untrusted-checkout-and-run`, `prt-no-permissions`
