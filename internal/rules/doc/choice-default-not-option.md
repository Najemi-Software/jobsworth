# choice-default-not-option

## What it checks

A `type: choice` input's own `default:` is not one of its `options:`.

## Why it matters

GitHub rejects the workflow at parse time.

## Example: flagged

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      level:
        type: choice
        options: [low, high]
        default: medium
jobs:
  build:
    runs-on: ubuntu-latest
    if: inputs.level == 'high'
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      level:
        type: choice
        options: [low, high]
        default: low
jobs:
  build:
    runs-on: ubuntu-latest
    if: inputs.level == 'high'
    steps:
      - run: echo ok
```

## When it does not fire

No default, or an expression default.

## Suppress

`# jobsworth-ignore choice-default-not-option` on the default's line, or
`rules: { choice-default-not-option: off }` in `.jobsworth.yml`.

## Related

`choice-no-options`, `input-default-type`.
