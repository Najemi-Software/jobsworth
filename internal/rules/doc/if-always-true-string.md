# if-always-true-string

## What it checks

An `if:` whose value is a bare word that is not `true` or `false` and not
an expression, such as `if: success`.

## Why it matters

A non-empty string is truthy, so the condition never prevents the step
from running. `success()` with parentheses was meant.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make
      - if: success
        run: echo ok
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make
      - if: success()
        run: echo ok
```

## When it does not fire

Any `if:` containing an operator, a call, or a context path.

## Suppress

`# jobsworth-ignore if-always-true-string` on the `if:` line, or
`rules: { if-always-true-string: off }` in `.jobsworth.yml`.

## Related

`if-double-wrapped`.
