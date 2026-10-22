# prt-no-permissions

## What it checks

A workflow on a privileged trigger with no `permissions:` block at either level.

## Why it matters

The write token is doubled risk there; name the least privilege the workflow needs.

## Example: flagged

```yaml
name: Example
on: pull_request_target
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
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
      - run: echo hi
```

## When it does not fire

Workflows with a permissions block somewhere.

## Suppress

`# jobsworth-ignore prt-no-permissions` on the line, or
`rules: { prt-no-permissions: off }` in `.jobsworth.yml`.

## Related

`permissions-missing`
