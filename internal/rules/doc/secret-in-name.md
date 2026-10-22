# secret-in-name

## What it checks

`secrets.*` appears in a step `name:`, job `name:`, or `run-name:`.

## Why it matters

Names are rendered in the UI and logs in clear; masking does not apply.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: deploy with ${{ secrets.TOKEN }}
        run: echo deploying
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - name: deploy
        run: echo deploying
        env:
          TOKEN: ${{ secrets.TOKEN }}
```

## When it does not fire

Secrets in `env:`, `with:`, or `run:`.

## Suppress

`# jobsworth-ignore secret-in-name` on the name's line, or
`rules: { secret-in-name: off }` in `.jobsworth.yml`.

## Related

`secret-to-output`.
