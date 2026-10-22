# secret-to-output

## What it checks

A job output's value (or a composite action output's value) is a
`secrets.*` expression.

## Why it matters

The runner masks secrets in outputs; consumers receive an empty string.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      token: ${{ secrets.TOKEN }}
    steps:
      - run: echo ok
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.token }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: x=${{ secrets.TOKEN }}
```

## When it does not fire

Secrets used directly in steps.

## Suppress

`# jobsworth-ignore secret-to-output` on the output's line, or
`rules: { secret-to-output: off }` in `.jobsworth.yml`.

## Related

`secret-in-name`.
