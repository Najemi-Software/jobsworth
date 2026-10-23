# secret-in-action

## What it checks

A composite action references `secrets.*`.

## Why it matters

The secrets context does not exist inside actions; the expression is
empty. Secrets must be passed in through `inputs`.

## Example: flagged

```yaml
description: Example
name: Deploy
runs:
  using: composite
  steps:
    - shell: bash
      run: x=${{ secrets.TOKEN }}
```

## Example: passes

```yaml
description: Example
name: Deploy
inputs:
  token:
    required: true
runs:
  using: composite
  steps:
    - shell: bash
      run: echo ${{ inputs.token }}
```

## When it does not fire

Workflows, where the context exists.

## Suppress

`# jobsworth-ignore secret-in-action` on the expression's line, or
`rules: { secret-in-action: off }` in `.jobsworth.yml`.

## Related

`secret-undeclared`.
