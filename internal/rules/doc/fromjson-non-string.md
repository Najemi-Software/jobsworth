# fromjson-non-string

## What it checks

`fromJSON(x)` where `x` is already an object or array, such as
`fromJSON(fromJSON(inputs.cfg))` or `fromJSON(matrix)`.

## Why it matters

`fromJSON` parses a string; given an object it fails at runtime. The inner
value was already structured.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ fromJSON(fromJSON('{"a":1}')).a }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ fromJSON('{"a":1}').a }}
```

## When it does not fire

`fromJSON` of a string whose JSON content is unknown is `any`;
`fromJSON(toJSON(x))` is the shape of `x`.

## Suppress

`# jobsworth-ignore fromjson-non-string` on the expression's line, or
`rules: { fromjson-non-string: off }` in `.jobsworth.yml`.

## Related

`shape-mismatch`.
