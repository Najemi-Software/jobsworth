# fromjson-invalid-literal

## What it checks

`fromJSON('...')` with a string literal that does not parse as JSON.

## Why it matters

The expression fails at runtime.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: |
          echo "${{ fromJSON('{version: 1}').version }}"
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: |
          echo "${{ fromJSON('{"version": 1}').version }}"
```

## When it does not fire

`fromJSON` of a context value; its content is checked by shape, not by
syntax.

## Suppress

`# jobsworth-ignore fromjson-invalid-literal` on the expression's line,
or `rules: { fromjson-invalid-literal: off }` in `.jobsworth.yml`.

## Related

`fromjson-non-string`.
