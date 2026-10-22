# expr-context-unavailable

## What it checks

A context used in a location where GitHub does not provide it: `steps` in a job-level `if:`, `runs-on` or job `env:`;
`matrix`, `needs`, `runner` or `job` at workflow level; `env` or `secrets` in `on:` or a workflow-level `concurrency:`;
`inputs` in `concurrency:`.

## Why it matters

The context is empty there, so the expression evaluates to nothing and the workflow behaves as if the value were blank.

## Example: flagged

```yaml
name: Example
on: push
env:
  OS: ${{ matrix.os }}
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

Contexts in the locations GitHub documents them for.

## Suppress

`# jobsworth-ignore expr-context-unavailable` on the line, or
`rules: { expr-context-unavailable: off }` in `.jobsworth.yml`.

## Related

`expr-env-in-if-job`, `expr-secrets-in-if`
