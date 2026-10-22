# composite-shell-missing

## What it checks

A composite action `run:` step has no `shell:`.

## Why it matters

GitHub rejects the action at runtime: composite steps have no default
shell.

## Example: flagged

```yaml
description: Example
name: Build
runs:
  using: composite
  steps:
    - run: make
```

## Example: passes

```yaml
description: Example
name: Build
runs:
  using: composite
  steps:
    - shell: bash
      run: make
```

## When it does not fire

Workflow steps default to bash and never need `shell:`.

## Suppress

`# jobsworth-ignore composite-shell-missing` on the `run:` line, or
`rules: { composite-shell-missing: off }` in `.jobsworth.yml`.

## Related

`bash-syntax`.
