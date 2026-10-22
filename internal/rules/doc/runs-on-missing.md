# runs-on-missing

## What it checks

A job with steps and no `runs-on:`.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

`uses:` jobs, which run on the callee's runners.

## Suppress

`# jobsworth-ignore runs-on-missing` on the line, or
`rules: { runs-on-missing: off }` in `.jobsworth.yml`.

## Related

`job-no-steps`
