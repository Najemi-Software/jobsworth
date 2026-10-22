# if-status-contradiction

## What it checks

Status functions that cannot hold together: `success() && failure()`, `!always()`, or `failure()` on a job with no
`needs:`.

## Why it matters

Nothing can satisfy them; the step or job never runs.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: success() && failure()
        run: echo never
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: failure()
        run: echo cleanup
```

## When it does not fire

A single status function, or `always()`.

## Suppress

`# jobsworth-ignore if-status-contradiction` on the line, or
`rules: { if-status-contradiction: off }` in `.jobsworth.yml`.

## Related

`if-always-false`
