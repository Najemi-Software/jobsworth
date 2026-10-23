# dispatch-input-type

## What it checks

`workflow_dispatch.inputs.X.type` not in `string`, `boolean`, `choice`, `environment`, `number`.

## Why it matters

GitHub rejects the workflow.

## Example: flagged

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      count:
        type: integer
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
  workflow_dispatch:
    inputs:
      count:
        type: number
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

The five documented types.

## Suppress

`# jobsworth-ignore dispatch-input-type` on the line, or
`rules: { dispatch-input-type: off }` in `.jobsworth.yml`.

## Related

`call-input-type-decl`
