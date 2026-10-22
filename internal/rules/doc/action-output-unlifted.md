# action-output-unlifted

## What it checks

A composite action's `outputs.<n>.value` references
`steps.X.outputs.Y`, and that step never writes `Y` (or the action it
uses declares no `Y`).

## Why it matters

Every caller of the action receives an empty output.

## Example: flagged

```yaml
description: Example
name: Meta
outputs:
  version:
    value: ${{ steps.v.outputs.version }}
runs:
  using: composite
  steps:
    - id: v
      shell: bash
      run: echo "version=1"
```

## Example: passes

```yaml
description: Example
name: Meta
outputs:
  version:
    value: ${{ steps.v.outputs.version }}
runs:
  using: composite
  steps:
    - id: v
      shell: bash
      run: echo "version=1" >> "$GITHUB_OUTPUT"
```

## When it does not fire

The same skip conditions as `step-output-unwritten`: open target,
non-bash shell, parse failure.

## Suppress

`# jobsworth-ignore action-output-unlifted` on the value's line, or
`rules: { action-output-unlifted: off }` in `.jobsworth.yml`.

## Related

`step-output-unwritten`, `step-output-action-undeclared`.
