# on-filter-glob

## What it checks

A filter pattern with an unbalanced `[` or a trailing backslash. Full glob-dialect validation is out of scope.

## Why it matters

The pattern matches nothing, or not what was meant.

## Example: flagged

```yaml
name: Example
on:
  push:
    branches: ['release/[0-9']
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
  push:
    branches: ['release/[0-9]*']
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Balanced brackets and escapes.

## Suppress

`# jobsworth-ignore on-filter-glob` on the line, or
`rules: { on-filter-glob: off }` in `.jobsworth.yml`.

## Related

`on-filter-expression`
