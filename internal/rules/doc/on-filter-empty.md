# on-filter-empty

## What it checks

`branches: []`, `paths: []` or any filter given as an empty list.

## Why it matters

An empty list matches nothing, so the event never triggers the workflow.

## Example: flagged

```yaml
name: Example
on:
  push:
    branches: []
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

Non-empty lists.

## Suppress

`# jobsworth-ignore on-filter-empty` on the line, or
`rules: { on-filter-empty: off }` in `.jobsworth.yml`.

## Related

`on-filter-shape`
