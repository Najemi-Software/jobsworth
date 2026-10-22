# step-run-and-uses

## What it checks

A step has both `run:` and `uses:`.

## Why it matters

GitHub rejects the workflow at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        run: make
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make
```

## When it does not fire

Every well-formed step.

## Suppress

`# jobsworth-ignore step-run-and-uses` on the `uses:` line, or
`rules: { step-run-and-uses: off }` in `.jobsworth.yml`.

## Related

`step-with-on-run`.
