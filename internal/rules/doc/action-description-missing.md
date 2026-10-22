# action-description-missing

## What it checks

An action with no `description:`.

## Why it matters

Required for the marketplace, and the action's documentation surface.

## Example: flagged

```yaml
name: Example
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

Actions with a description.

## Suppress

`# jobsworth-ignore action-description-missing` on the line, or
`rules: { action-description-missing: off }` in `.jobsworth.yml`.

## Related

`action-name-missing`
