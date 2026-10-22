# permissions-unknown-scope

## What it checks

A `permissions:` key that is not a documented scope.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on: push
permissions:
  content: read
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

The documented scopes.

## Suppress

`# jobsworth-ignore permissions-unknown-scope` on the line, or
`rules: { permissions-unknown-scope: off }` in `.jobsworth.yml`.

## Related

`permissions-unknown-level`
