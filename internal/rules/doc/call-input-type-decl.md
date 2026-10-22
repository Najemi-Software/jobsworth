# call-input-type-decl

## What it checks

`workflow_call.inputs.X.type` not in `string`, `boolean`, `number`. `choice` is dispatch-only and the most common
confusion.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    inputs:
      level:
        type: choice
        options: [low, high]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.level }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    inputs:
      level:
        type: string
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.level }}
```

## When it does not fire

The three documented types.

## Suppress

`# jobsworth-ignore call-input-type-decl` on the line, or
`rules: { call-input-type-decl: off }` in `.jobsworth.yml`.

## Related

`dispatch-input-type`
