# permissions-write-all

## What it checks

`permissions: write-all` at workflow or job level.

## Why it matters

Every scope is granted; list the ones the workflow needs.

## Example: flagged

```yaml
name: Example
on: push
permissions: write-all
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
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Explicit scope maps and `read-all`.

## Suppress

`# jobsworth-ignore permissions-write-all` on the line, or
`rules: { permissions-write-all: off }` in `.jobsworth.yml`.

## Related

`permissions-missing`
