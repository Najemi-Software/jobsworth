# environment-no-protection

<!-- jobsworth-test: enrich self=example/app -->

## What it checks

A deployment environment with no required reviewers or branch policy, used by a workflow on a privileged trigger.
Enrich-only.

## Why it matters

Attacker-triggered runs deploy without anyone approving.

## Example: flagged

```yaml
name: Example
on: pull_request_target
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    environment: staging
    steps:
      - run: echo deploy
```

## Example: passes

```yaml
name: Example
on: pull_request_target
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - run: echo deploy
```

## When it does not fire

Protected environments and unprivileged triggers.

## Suppress

`# jobsworth-ignore environment-no-protection` on the line, or
`rules: { environment-no-protection: off }` in `.jobsworth.yml`.

## Related

`environment-unknown`
