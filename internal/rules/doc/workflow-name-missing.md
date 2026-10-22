# workflow-name-missing

## What it checks

A workflow with no `name:`.

## Why it matters

The Actions tab shows the file path instead.

## Example: flagged

```yaml
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

Workflows with a name.

## Suppress

`# jobsworth-ignore workflow-name-missing` on the line, or
`rules: { workflow-name-missing: off }` in `.jobsworth.yml`.

## Related

`workflow-name-duplicate`
