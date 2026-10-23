# if-not-boolean

## What it checks

An `if:` whose top-level value is not a boolean: a bare string input, a `steps.X.outputs.Y` (always a string), `env.X`,
a string literal, a number. Mixed-text conditions and unknown shapes are skipped.

## Why it matters

Any non-empty string is truthy, including `'false'`, so the condition does not do what it reads as. Compare it, e.g.
`== 'true'`.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    inputs:
      name:
        type: string
      flag:
        type: boolean
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: inputs.name
        run: echo named ${{ inputs.flag }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    inputs:
      name:
        type: string
      flag:
        type: boolean
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: inputs.name != ''
        run: echo named ${{ inputs.flag }}
```

## When it does not fire

Boolean inputs, comparisons, status functions.

## Suppress

`# jobsworth-ignore if-not-boolean` on the line, or
`rules: { if-not-boolean: off }` in `.jobsworth.yml`.

## Related

`if-string-compare-bool`, `if-always-true-string`
