# expr-comparison-type

## What it checks

`==` or `!=` between a value inferred to be a number and a string literal that is not numeric, or between a boolean and
a string other than `'true'`/`'false'`.

## Why it matters

GitHub coerces both sides; a non-numeric string becomes NaN and never equals a number, so the comparison is dead.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    inputs:
      count:
        type: number
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.count == 'many' }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    inputs:
      count:
        type: number
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.count == '3' }}
```

## When it does not fire

Comparisons whose non-literal side has an unknown or string shape.

## Suppress

`# jobsworth-ignore expr-comparison-type` on the line, or
`rules: { expr-comparison-type: off }` in `.jobsworth.yml`.

## Related

`value-set-mismatch`
