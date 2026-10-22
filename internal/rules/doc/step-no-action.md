# step-no-action

## What it checks

A step with neither `run:` nor `uses:`.

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
      - name: nothing
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

Steps with one of the two.

## Suppress

`# jobsworth-ignore step-no-action` on the line, or
`rules: { step-no-action: off }` in `.jobsworth.yml`.

## Related

`step-null`
