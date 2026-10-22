# action-name-missing

## What it checks

An action with no `name:`.

## Why it matters

Required for the marketplace, and the action's documentation surface.

## Example: flagged

```yaml
description: Example
runs:
  using: composite
  steps:
    - shell: bash
      run: echo hi
```

## Example: passes

```yaml
name: Example
description: Example
runs:
  using: composite
  steps:
    - shell: bash
      run: echo hi
```

## When it does not fire

Actions with a name.

## Suppress

`# jobsworth-ignore action-name-missing` on the line, or
`rules: { action-name-missing: off }` in `.jobsworth.yml`.

## Related

`action-description-missing`
