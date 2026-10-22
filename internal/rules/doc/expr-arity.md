# expr-arity

## What it checks

An expression function called with the wrong number of arguments: `contains`, `startsWith`, `endsWith` take two; `join`
one or two; `toJSON`, `fromJSON` one; `hashFiles` and `format` one or more; the status functions none.

## Why it matters

GitHub rejects the workflow at load time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ contains(github.ref) }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ contains(github.ref, 'main') }}
```

## When it does not fire

Correct counts, including the optional separator of `join` and extra `hashFiles` patterns.

## Suppress

`# jobsworth-ignore expr-arity` on the line, or
`rules: { expr-arity: off }` in `.jobsworth.yml`.

## Related

`expr-unknown-function`, `expr-format-placeholders`
