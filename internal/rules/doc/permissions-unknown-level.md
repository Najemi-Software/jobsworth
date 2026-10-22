# permissions-unknown-level

## What it checks

A `permissions:` value other than `read`, `write` or `none`.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on: push
permissions:
  contents: readonly
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

`read`, `write`, `none`.

## Suppress

`# jobsworth-ignore permissions-unknown-level` on the line, or
`rules: { permissions-unknown-level: off }` in `.jobsworth.yml`.

## Related

`permissions-unknown-scope`
