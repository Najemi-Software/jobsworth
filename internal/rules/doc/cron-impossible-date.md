# cron-impossible-date

## What it checks

Day-of-month 30 or 31 with a month list that never has it (`0 0 31 2 *`), or `29 2` alone.

## Why it matters

The schedule never fires, or fires only in leap years.

## Example: flagged

```yaml
name: Example
on:
  schedule:
    - cron: '0 0 31 2 *'
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
    - cron: '0 0 28 2 *'
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Dates every listed month has.

## Suppress

`# jobsworth-ignore cron-impossible-date` on the line, or
`rules: { cron-impossible-date: off }` in `.jobsworth.yml`.

## Related

`cron-syntax`
