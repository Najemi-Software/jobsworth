# if-always-false

## What it checks

An `if:` that evaluates to false under every assignment: same-operand contradictions (`x == 'a' && x == 'b'`), literal
comparisons that fail, and combinations of those.

## Why it matters

The step or job never runs. A literal `if: false` is reported as `if-literal-false` instead, so the deliberate case is
configurable apart.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: github.ref == 'refs/heads/main' && github.ref == 'refs/heads/dev'
        run: echo never
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
      - if: github.ref == 'refs/heads/main' || github.ref == 'refs/heads/dev'
        run: echo sometimes
```

## When it does not fire

Conditions with any unknown operand, and provably-true conditions.

## Suppress

`# jobsworth-ignore if-always-false` on the line, or
`rules: { if-always-false: off }` in `.jobsworth.yml`.

## Related

`if-literal-false`, `if-status-contradiction`
