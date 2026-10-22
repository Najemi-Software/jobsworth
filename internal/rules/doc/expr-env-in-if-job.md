# expr-env-in-if-job

## What it checks

`env.*` inside a job-level `if:`.

## Why it matters

The `env` context is not available in job-level conditions; the value is empty and the comparison fails silently.
Step-level `if:` does see `env`.

## Example: flagged

```yaml
name: Example
on: push
env:
  DEPLOY: 'true'
jobs:
  build:
    runs-on: ubuntu-latest
    if: env.DEPLOY == 'true'
    steps:
      - run: echo deploy
```

## Example: passes

```yaml
name: Example
on: push
env:
  DEPLOY: 'true'
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: env.DEPLOY == 'true'
        run: echo deploy
```

## When it does not fire

`env.*` in a step-level `if:`.

## Suppress

`# jobsworth-ignore expr-env-in-if-job` on the line, or
`rules: { expr-env-in-if-job: off }` in `.jobsworth.yml`.

## Related

`expr-context-unavailable`
