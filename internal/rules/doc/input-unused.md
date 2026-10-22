# input-unused

## What it checks

An input is declared but no expression anywhere in the file references it.

## Why it matters

Callers keep passing a value nobody reads, and the declaration misleads
anyone reading the interface. Usually a leftover from a refactor.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    inputs:
      env: {type: string}
      verbose: {type: boolean}
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.env }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    inputs:
      env: {type: string}
      verbose: {type: boolean}
jobs:
  deploy:
    runs-on: ubuntu-latest
    if: inputs.verbose
    steps:
      - run: echo ${{ inputs.env }}
```

## When it does not fire

Dispatch inputs of a workflow with no `workflow_call` trigger (they are
often read through `github.event.inputs`, which is not tracked), and
inputs of non-composite actions (their implementation is never inspected).

## Suppress

`# jobsworth-ignore input-unused` on the input's line, or
`rules: { input-unused: off }` in `.jobsworth.yml`.

## Related

`input-undeclared`.
