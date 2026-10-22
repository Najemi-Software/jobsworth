# expr-contains-types

## What it checks

`contains(x, y)` where `x` is inferred to be an object. `contains` searches strings and arrays only.

## Why it matters

On an object the result is always false, so the condition it guards never holds.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ contains(fromJSON('{"a":1}'), 'a') }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ contains(fromJSON('["a"]'), 'a') }}
```

## When it does not fire

A first argument whose shape is unknown, a string, or an array.

## Suppress

`# jobsworth-ignore expr-contains-types` on the line, or
`rules: { expr-contains-types: off }` in `.jobsworth.yml`.

## Related

`shape-mismatch`
