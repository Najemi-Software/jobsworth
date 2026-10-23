# on-workflow-run-target

## What it checks

`workflow_run` without a `workflows:` list.

## Why it matters

GitHub requires the triggering workflows to be named; the file is rejected.

## Example: flagged

```yaml
name: Example
on:
  workflow_run:
    types: [completed]
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
  workflow_run:
    workflows: [Reusable]
    types: [completed]
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

`workflow_run` with `workflows:`.

## Suppress

`# jobsworth-ignore on-workflow-run-target` on the line, or
`rules: { on-workflow-run-target: off }` in `.jobsworth.yml`.

## Related

`on-workflow-run-unknown`
