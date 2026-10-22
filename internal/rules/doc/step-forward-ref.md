# step-forward-ref

## What it checks

A step references `steps.X.outputs.*` where `X` is declared later in the
same scope, or is the step itself.

## Why it matters

Steps run in order; the value is always empty when the reference is
evaluated. A job's `outputs:` mapping and an action's `outputs.*.value`
run after every step and may reference any id.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ steps.meta.outputs.version }}
      - id: meta
        run: echo "version=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: ${{ steps.meta.outputs.version }}
    steps:
      - id: meta
        run: echo "version=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.version }}
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.version }}
```

## When it does not fire

References from `outputs:` blocks, and references to earlier steps.

## Suppress

`# jobsworth-ignore step-forward-ref` on the expression's line, or
`rules: { step-forward-ref: off }` in `.jobsworth.yml`.

## Related

`step-id-unknown`.
