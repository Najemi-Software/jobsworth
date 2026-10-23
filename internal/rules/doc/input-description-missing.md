# input-description-missing

<!-- jobsworth-test: enable -->

## What it checks

An input without `description:` on an action or a reusable workflow.

## Why it matters

The description is what callers see. Off by default because it floods legacy repositories.

## Example: flagged

```yaml
name: Example
description: Example
inputs:
  who:
    required: true
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ${{ inputs.who }}
```

## Example: passes

```yaml
name: Example
description: Example
inputs:
  who:
    description: who to greet
    required: true
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ${{ inputs.who }}
```

## When it does not fire

Inputs with a description, and `workflow_dispatch` inputs.

## Suppress

`# jobsworth-ignore input-description-missing` on the line, or
`rules: { input-description-missing: off }` in `.jobsworth.yml`.

## Related

None.
