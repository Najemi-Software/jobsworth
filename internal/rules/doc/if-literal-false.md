# if-literal-false

## What it checks

`if: false` on a step or job.

## Why it matters

The canonical deliberate disablement; reported as a note so it stays visible without failing anything.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: false
        run: echo disabled
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
      - run: echo enabled
```

## When it does not fire

Any other condition.

## Suppress

`# jobsworth-ignore if-literal-false` on the line, or
`rules: { if-literal-false: off }` in `.jobsworth.yml`.

## Related

`if-always-false`, `job-unreachable`
