# on-filter-shape

## What it checks

A filter value that is not a scalar string or a list of scalar strings.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on:
  push:
    branches:
      main: true
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
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Strings and lists of strings.

## Suppress

`# jobsworth-ignore on-filter-shape` on the line, or
`rules: { on-filter-shape: off }` in `.jobsworth.yml`.

## Related

`on-filter-conflict`, `on-filter-empty`
