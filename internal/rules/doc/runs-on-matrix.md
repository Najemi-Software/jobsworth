# runs-on-matrix

## What it checks

`runs-on: ${{ matrix.os }}` where the axis holds an unknown or retired label.

## Why it matters

Those matrix legs never schedule.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    strategy:
      matrix:
        os: [ubuntu-latest, ubuntu-20.04]
    runs-on: ${{ matrix.os }}
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    strategy:
      matrix:
        os: [ubuntu-latest, windows-latest]
    runs-on: ${{ matrix.os }}
    steps:
      - run: echo hi
```

## When it does not fire

Expression-valued `runs-on` not backed by a matrix axis.

## Suppress

`# jobsworth-ignore runs-on-matrix` on the line, or
`rules: { runs-on-matrix: off }` in `.jobsworth.yml`.

## Related

`runs-on-unknown`, `runs-on-retired`
