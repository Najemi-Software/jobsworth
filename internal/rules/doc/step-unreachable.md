# step-unreachable

## What it checks

A step whose `if:` is always false (§4.2) and whose outputs a later step consumes.

## Why it matters

The consumer runs with empty values. The bare always-false case is `if-always-false`; this one says the dead step has
dependants.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: v
        if: github.ref == 'a' && github.ref == 'b'
        run: echo "version=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.v.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: v
        if: github.ref == 'refs/heads/main'
        run: echo "version=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.v.outputs.version }}
```

## When it does not fire

Steps whose condition can hold, and dead steps nothing consumes.

## Suppress

`# jobsworth-ignore step-unreachable` on the line, or
`rules: { step-unreachable: off }` in `.jobsworth.yml`.

## Related

`if-always-false`
