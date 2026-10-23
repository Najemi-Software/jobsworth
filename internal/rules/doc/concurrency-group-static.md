# concurrency-group-static

## What it checks

A `concurrency.group` that is a literal with no expression, on a `pull_request` or `push` workflow.

## Why it matters

Every run of the workflow across all branches cancels or queues behind every other;
`${{ github.workflow }}-${{ github.ref }}` is almost always what was meant.

## Example: flagged

```yaml
name: Example
on: push
concurrency: ci
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
concurrency: ${{ github.workflow }}-${{ github.ref }}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Groups with an expression, and workflows on other triggers.

## Suppress

`# jobsworth-ignore concurrency-group-static` on the line, or
`rules: { concurrency-group-static: off }` in `.jobsworth.yml`.

## Related

`concurrency-cancel-on-push-main`
