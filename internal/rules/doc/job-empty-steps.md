# job-empty-steps

## What it checks

`steps: []` on a job without `uses:`.

## Why it matters

The job runs, occupies a runner, and does nothing. Distinguished from a missing `steps:` key.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps: []
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

Jobs with at least one step.

## Suppress

`# jobsworth-ignore job-empty-steps` on the line, or
`rules: { job-empty-steps: off }` in `.jobsworth.yml`.

## Related

`job-no-steps`
