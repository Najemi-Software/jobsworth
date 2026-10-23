# expr-or-default-boolean

## What it checks

`inputs.flag || 'default'` where `flag` is `type: boolean`.

## Why it matters

`false || 'default'` yields `'default'`; the idiom silently drops `false`. Use
`inputs.flag != '' && inputs.flag || 'default'` or a ternary form.

## Example: flagged

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      flag:
        type: boolean
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.flag || 'default' }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      flag:
        type: boolean
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ inputs.flag && 'on' || 'off' }}
```

## When it does not fire

String inputs, which are never dropped.

## Suppress

`# jobsworth-ignore expr-or-default-boolean` on the line, or
`rules: { expr-or-default-boolean: off }` in `.jobsworth.yml`.

## Related

`expr-and-or-ternary-falsy`
