# action-output-unused

## What it checks

A local composite action declares an output that no workflow or action in
the entry repo or any linked repo consumes through
`steps.<id>.outputs.<name>`.

## Why it matters

Hygiene: the output is dead, or its consumer lives in a repo that is not
linked. A note, never a failure.

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
      run: echo "version=1" >> "$GITHUB_OUTPUT"
```

## Example: passes

```yaml
description: Example
name: Greet
inputs:
  name:
    required: true
runs:
  using: composite
  steps:
    - shell: bash
      run: echo "hello ${{ inputs.name }}"
```

## When it does not fire

Fetched actions are never scanned; a consumer anywhere in a locally known
repo counts.

## Suppress

`# jobsworth-ignore action-output-unused` on the output's line, or
`rules: { action-output-unused: off }` in `.jobsworth.yml`.

## Related

`output-unused`.
