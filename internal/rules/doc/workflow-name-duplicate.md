# workflow-name-duplicate

## What it checks

Two workflows in the entry repository with the same `name:`. Reported on the later file in path order, naming the
earlier.

## Why it matters

The Actions tab cannot tell them apart.

## Example: flagged

```yaml
name: Choosy
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Unique names, and fetched files.

## Suppress

`# jobsworth-ignore workflow-name-duplicate` on the line, or
`rules: { workflow-name-duplicate: off }` in `.jobsworth.yml`.

## Related

`workflow-name-missing`
