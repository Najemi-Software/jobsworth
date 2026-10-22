# expr-and-or-ternary-falsy

## What it checks

`cond && x || y` where `x` can be falsy: `''`, `0`, `false`, or a boolean input.

## Why it matters

The ternary idiom yields `y` when `cond` is true and `x` is falsy.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ github.event_name == 'push' && '' || 'other' }}
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
      - run: echo ${{ github.event_name == 'push' && 'pushed' || 'other' }}
```

## When it does not fire

A middle operand that is a non-empty literal or a string-shaped value.

## Suppress

`# jobsworth-ignore expr-and-or-ternary-falsy` on the line, or
`rules: { expr-and-or-ternary-falsy: off }` in `.jobsworth.yml`.

## Related

`expr-or-default-boolean`
