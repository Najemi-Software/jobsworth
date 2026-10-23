# matrix-empty

## What it checks

A `strategy.matrix` axis given as an empty list.

## Why it matters

The job has zero combinations and never runs.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        os: []
    steps:
      - run: echo ${{ matrix.os }}
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        os: [ubuntu-latest]
    steps:
      - run: echo ${{ matrix.os }}
```

## When it does not fire

Axes with values, and expression-valued axes.

## Suppress

`# jobsworth-ignore matrix-empty` on the line, or
`rules: { matrix-empty: off }` in `.jobsworth.yml`.

## Related

`matrix-exclude-no-match`
