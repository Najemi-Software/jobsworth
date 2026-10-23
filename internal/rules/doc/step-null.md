# step-null

## What it checks

A `null` entry in a steps list, usually a stray `-`.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
      -
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

Lists whose every item is a mapping.

## Suppress

`# jobsworth-ignore step-null` on the line, or
`rules: { step-null: off }` in `.jobsworth.yml`.

## Related

`step-no-action`
