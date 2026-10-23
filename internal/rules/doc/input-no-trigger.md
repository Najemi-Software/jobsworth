# input-no-trigger

## What it checks

`inputs.X` referenced in a workflow that has neither `workflow_call` nor `workflow_dispatch`.

## Why it matters

The `inputs` context is always empty there; the reference silently yields nothing. (`input-undeclared` assumes a
declaring trigger exists; this completes it.)

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.name }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      name:
        type: string
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.name }}
```

## When it does not fire

Workflows with either trigger, and actions.

## Suppress

`# jobsworth-ignore input-no-trigger` on the line, or
`rules: { input-no-trigger: off }` in `.jobsworth.yml`.

## Related

`input-undeclared`
