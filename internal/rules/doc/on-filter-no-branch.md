# on-filter-no-branch

<!-- jobsworth-test: enrich self=example/app -->

## What it checks

A literal (non-glob) `branches:` entry naming a branch the repository does not have. Enrich-only.

## Why it matters

The filter matches nothing until someone creates the branch.

## Example: flagged

```yaml
name: Example
on:
  push:
    branches: [release]
permissions: read-all
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
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Glob patterns and existing branches.

## Suppress

`# jobsworth-ignore on-filter-no-branch` on the line, or
`rules: { on-filter-no-branch: off }` in `.jobsworth.yml`.

## Related

`on-filter-glob`
