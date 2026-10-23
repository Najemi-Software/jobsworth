# input-default-type

## What it checks

A declared `default:` cannot be represented in the declared `type:`: a
non-numeric default for `number`, anything but a bare `true`/`false` for
`boolean`, or a `choice` default outside `options:`.

## Why it matters

GitHub rejects the workflow at parse time, so every run that would have
used the default fails before any job starts.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    inputs:
      retries: {type: number, default: three}
jobs:
  build:
    runs-on: ubuntu-latest
    if: inputs.retries
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    inputs:
      retries: {type: number, default: 3}
jobs:
  build:
    runs-on: ubuntu-latest
    if: inputs.retries
    steps:
      - run: echo ok
```

## When it does not fire

The default is an expression (`${{ ... }}`), the type is `string` or
undeclared, or the input belongs to an action (actions have no types).

## Suppress

`# jobsworth-ignore input-default-type` on the default's line, or
`rules: { input-default-type: off }` in `.jobsworth.yml`.

## Related

`call-input-type`.
