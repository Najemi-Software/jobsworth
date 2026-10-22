# job-timeout-missing

<!-- jobsworth-test: enable -->

## What it checks

A job with no `timeout-minutes:`.

## Why it matters

The default is 360 minutes. Off by default; opt in for teams that enforce explicit timeouts.

## Example: flagged

```yaml
name: Example
on: push
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
jobs:
  build:
    runs-on: ubuntu-latest
    timeout-minutes: 30
    steps:
      - run: echo hi
```

## When it does not fire

Jobs with a timeout, and `uses:` jobs.

## Suppress

`# jobsworth-ignore job-timeout-missing` on the line, or
`rules: { job-timeout-missing: off }` in `.jobsworth.yml`.

## Related

None.
