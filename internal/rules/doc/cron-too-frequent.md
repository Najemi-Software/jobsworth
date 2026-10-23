# cron-too-frequent

## What it checks

A schedule whose minute field fires more often than every 5 minutes.

## Why it matters

GitHub throttles schedules to every 5 minutes; the extra runs never happen.

## Example: flagged

```yaml
name: Example
on:
  schedule:
    - cron: '*/2 * * * *'
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on:
  schedule:
    - cron: '*/10 * * * *'
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Intervals of 5 minutes or more.

## Suppress

`# jobsworth-ignore cron-too-frequent` on the line, or
`rules: { cron-too-frequent: off }` in `.jobsworth.yml`.

## Related

`cron-syntax`
