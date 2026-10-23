# matrix-exclude-no-match

## What it checks

An `exclude:` entry that matches no axis combination.

## Why it matters

The entry is dead; usually a typo in a value, so the combination it meant to drop still runs.

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
        os: [ubuntu-latest, windows-latest]
        exclude:
          - os: macos-latest
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
        os: [ubuntu-latest, windows-latest]
        exclude:
          - os: windows-latest
    steps:
      - run: echo ${{ matrix.os }}
```

## When it does not fire

Entries that match at least one combination, and expression-valued matrices.

## Suppress

`# jobsworth-ignore matrix-exclude-no-match` on the line, or
`rules: { matrix-exclude-no-match: off }` in `.jobsworth.yml`.

## Related

`matrix-empty`
