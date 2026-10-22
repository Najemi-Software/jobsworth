# if-double-wrapped

## What it checks

An `if:` that contains two `${{ }}` blocks, such as
`if: "${{ a }} && ${{ b }}"`.

## Why it matters

The two results are rendered into one string with ` && ` between them,
and a non-empty string is always truthy. Wrap the whole condition in one
`${{ }}` or drop the braces.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.event_name == 'push' }} && ${{ runner.os == 'Linux' }}
        run: echo ok
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: ${{ github.event_name == 'push' && runner.os == 'Linux' }}
        run: echo ok
```

## When it does not fire

A single expression block, or none.

## Suppress

`# jobsworth-ignore if-double-wrapped` on the `if:` line, or
`rules: { if-double-wrapped: off }` in `.jobsworth.yml`.

## Related

`if-always-true-string`.
