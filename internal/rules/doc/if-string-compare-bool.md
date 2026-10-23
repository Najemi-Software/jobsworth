# if-string-compare-bool

## What it checks

`steps.X.outputs.Y == true` (or `false`): the output is a string, and `'true' == true` is false.

## Why it matters

The branch never runs; compare with the string literal `'true'`.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: x
        run: echo "flag=true" >> "$GITHUB_OUTPUT"
      - if: steps.x.outputs.flag == true
        run: echo yes
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
      - id: x
        run: echo "flag=true" >> "$GITHUB_OUTPUT"
      - if: steps.x.outputs.flag == 'true'
        run: echo yes
```

## When it does not fire

Boolean inputs compared with booleans.

## Suppress

`# jobsworth-ignore if-string-compare-bool` on the line, or
`rules: { if-string-compare-bool: off }` in `.jobsworth.yml`.

## Related

`if-not-boolean`
