# job-unreachable

## What it checks

A job whose `needs:` chain passes through a job with a literal `if: false`, with no `always()` or `!cancelled()` on the
way.

## Why it matters

A skipped dependency skips every dependant; the job never runs.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  gate:
    runs-on: ubuntu-latest
    if: false
    steps:
      - run: echo gate
  build:
    runs-on: ubuntu-latest
    needs: gate
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  gate:
    runs-on: ubuntu-latest
    if: false
    steps:
      - run: echo gate
  build:
    runs-on: ubuntu-latest
    needs: gate
    if: always()
    steps:
      - run: echo hi
```

## When it does not fire

Chains with `always()` or `!cancelled()` on a job between. A dependency counts as dead when its `if:` is literally
`false` or provably false (`success() && failure()`), as for `if-always-false`.

## Suppress

`# jobsworth-ignore job-unreachable` on the line, or
`rules: { job-unreachable: off }` in `.jobsworth.yml`.

## Related

`if-literal-false`
