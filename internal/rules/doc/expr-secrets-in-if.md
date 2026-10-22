# expr-secrets-in-if

## What it checks

`secrets.*` inside any `if:` condition.

## Why it matters

The `secrets` context is not available in conditions; the expression is empty. Pass the secret through `env:` and test
the environment variable in a `run:` step instead.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: secrets.TOKEN != ''
        run: echo has token
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - env:
          HAS_TOKEN: ${{ secrets.TOKEN != '' }}
        run: echo "$HAS_TOKEN"
```

## When it does not fire

`secrets.*` in `env:`, `with:` or `run:`.

## Suppress

`# jobsworth-ignore expr-secrets-in-if` on the line, or
`rules: { expr-secrets-in-if: off }` in `.jobsworth.yml`.

## Related

`expr-context-unavailable`
