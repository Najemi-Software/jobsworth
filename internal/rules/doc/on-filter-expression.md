# on-filter-expression

## What it checks

`${{` inside an `on:` filter pattern.

## Why it matters

Expressions are not evaluated in `on:`; the pattern is matched literally and never matches a real branch.

## Example: flagged

```yaml
name: Example
on:
  push:
    branches: ['${{ github.ref }}']
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on:
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Literal patterns.

## Suppress

`# jobsworth-ignore on-filter-expression` on the line, or
`rules: { on-filter-expression: off }` in `.jobsworth.yml`.

## Related

`on-filter-glob`
