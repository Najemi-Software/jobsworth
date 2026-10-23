# step-id-unknown

## What it checks

`steps.X.outputs.Y`, `steps.X.outcome` or `steps.X.conclusion` is
referenced in a job or composite action, but no step in that scope has
`id: X`.

## Why it matters

The expression evaluates to empty at runtime; usually a renamed or
forgotten `id:`. The message lists the ids that are declared.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "version=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.metadata.outputs.version }} ${{ steps.meta.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "version=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.version }}
```

## When it does not fire

Only `steps.*` paths are checked; `needs.*` has its own rules.

## Suppress

`# jobsworth-ignore step-id-unknown` on the expression's line, or
`rules: { step-id-unknown: off }` in `.jobsworth.yml`.

## Related

`step-forward-ref`.
