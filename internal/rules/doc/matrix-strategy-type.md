# matrix-strategy-type

## What it checks

`strategy.max-parallel` that is not a number, or `strategy.fail-fast` that is not a boolean.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      max-parallel: two
      matrix:
        os: [ubuntu-latest]
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
      max-parallel: 2
      fail-fast: false
      matrix:
        os: [ubuntu-latest]
    steps:
      - run: echo ${{ matrix.os }}
```

## When it does not fire

Numbers, booleans and expressions.

## Suppress

`# jobsworth-ignore matrix-strategy-type` on the line, or
`rules: { matrix-strategy-type: off }` in `.jobsworth.yml`.

## Related

`matrix-empty`
