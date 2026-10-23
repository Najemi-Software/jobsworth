# dispatch-input-event-spelling

<!-- jobsworth-test: enable -->

## What it checks

`github.event.inputs.X` used where `inputs.X` would do.

## Why it matters

Both work under `workflow_dispatch`; `inputs.X` also works under `workflow_call`, so it is the portable spelling. Off by
default.

## Example: flagged

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
      - run: echo ${{ github.event.inputs.name }}
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

`inputs.X`.

## Suppress

`# jobsworth-ignore dispatch-input-event-spelling` on the line, or
`rules: { dispatch-input-event-spelling: off }` in `.jobsworth.yml`.

## Related

None.
