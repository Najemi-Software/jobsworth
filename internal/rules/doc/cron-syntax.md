# cron-syntax

## What it checks

A `schedule:` entry that is not a `cron:` mapping, or a cron expression that is not five fields of `*`, values, `a-b`,
`*/n`, `a-b/n` or comma lists, with values in range (`0-59`, `0-23`, `1-31`, `1-12` or `JAN-DEC`, `0-6` or `SUN-SAT`).

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on:
  schedule:
    - cron: '0 0 * *'
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
    - cron: '0 0 * * *'
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Valid five-field expressions.

## Suppress

`# jobsworth-ignore cron-syntax` on the line, or
`rules: { cron-syntax: off }` in `.jobsworth.yml`.

## Related

`cron-too-frequent`, `cron-impossible-date`
