# on-filter-conflict

## What it checks

`branches` with `branches-ignore`, `paths` with `paths-ignore`, or `tags` with `tags-ignore` on one event.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on:
  push:
    branches: [main]
    branches-ignore: [dev]
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

One of each pair.

## Suppress

`# jobsworth-ignore on-filter-conflict` on the line, or
`rules: { on-filter-conflict: off }` in `.jobsworth.yml`.

## Related

`on-filter-shape`
