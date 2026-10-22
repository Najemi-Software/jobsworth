# on-workflow-run-unknown

## What it checks

`workflow_run.workflows` names a workflow `name:` that no workflow file in the repository declares. Only when the
repository is local (the entry repo or a linked one).

## Why it matters

The trigger never fires. Workflows are matched by their `name:`, not their file name.

## Example: flagged

```yaml
name: Example
on:
  workflow_run:
    workflows: [Nope]
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

Fetched workflows, and names that match a local workflow's `name:`.

## Suppress

`# jobsworth-ignore on-workflow-run-unknown` on the line, or
`rules: { on-workflow-run-unknown: off }` in `.jobsworth.yml`.

## Related

`on-workflow-run-target`
