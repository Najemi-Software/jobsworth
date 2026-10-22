# expr-string-escape

## What it checks

A double-quoted string literal inside an expression, or a single-quoted literal with an unescaped `'` (it must be
written as `''`).

## Why it matters

GitHub expressions only have single-quoted strings; the file is rejected at load time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ github.ref == "refs/heads/main" }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ github.ref == 'refs/heads/main' }}
```

## When it does not fire

Single-quoted strings with `''` for an embedded quote.

## Suppress

`# jobsworth-ignore expr-string-escape` on the line, or
`rules: { expr-string-escape: off }` in `.jobsworth.yml`.

## Related

None.
