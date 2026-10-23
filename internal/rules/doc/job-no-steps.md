# job-no-steps

## What it checks

A job with neither `steps:` nor `uses:`.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
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

Jobs with steps or a `uses:`.

## Suppress

`# jobsworth-ignore job-no-steps` on the line, or
`rules: { job-no-steps: off }` in `.jobsworth.yml`.

## Related

`job-empty-steps`, `runs-on-missing`
