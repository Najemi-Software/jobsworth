# choice-no-options

## What it checks

A `type: choice` input with empty or missing `options:`.

## Why it matters

GitHub rejects the workflow at parse time; a choice needs choices.

## Example: flagged

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      level:
        type: choice
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
jobs:
  build:
    runs-on: ubuntu-latest
    if: inputs.level == 'high'
    steps:
      - run: echo ok
```

## When it does not fire

Inputs of any other type.

## Suppress

`# jobsworth-ignore choice-no-options` on the input's line, or
`rules: { choice-no-options: off }` in `.jobsworth.yml`.

## Related

`choice-default-not-option`.
