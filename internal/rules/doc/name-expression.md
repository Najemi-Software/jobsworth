# name-expression

## What it checks

A workflow-level `name:` containing `${{`.

## Why it matters

The workflow name is not evaluated; the expression is displayed verbatim. `run-name:`, job `name:` (the documented way
to label matrix legs) and step `name:` do evaluate expressions.

## Example: flagged

```yaml
name: Build ${{ github.ref }}
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Build
run-name: Build ${{ github.ref }}
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

`run-name:` and step names.

## Suppress

`# jobsworth-ignore name-expression` on the line, or
`rules: { name-expression: off }` in `.jobsworth.yml`.

## Related

None.
