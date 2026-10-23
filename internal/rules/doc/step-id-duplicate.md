# step-id-duplicate

## What it checks

Two steps in one job or composite action share an `id:`.

## Why it matters

GitHub rejects the workflow at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "a=1" >> "$GITHUB_OUTPUT"
      - id: meta
        run: echo "a=2" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.a }}
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
        run: echo "a=1" >> "$GITHUB_OUTPUT"
      - id: more
        run: echo "b=2" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.a }} ${{ steps.more.outputs.b }}
```

## When it does not fire

The same id in two different jobs is fine; ids are per scope.

## Suppress

`# jobsworth-ignore step-id-duplicate` on the second id's line, or
`rules: { step-id-duplicate: off }` in `.jobsworth.yml`.

## Related

`step-id-invalid`.
