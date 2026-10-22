# secret-echo

## What it checks

`echo ${{ secrets.X }}`, or `echo "$X"` where `X` is seeded from a secret.

## Why it matters

The runner masks exact matches only; transformed values leak. Still a smell even when masked.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ secrets.TOKEN }}
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
      - env:
          TOKEN: ${{ secrets.TOKEN }}
        run: 'curl -H "Authorization: $TOKEN" https://example.test'
```

## When it does not fire

Secrets used without echoing them.

## Suppress

`# jobsworth-ignore secret-echo` on the line, or
`rules: { secret-echo: off }` in `.jobsworth.yml`.

## Related

`secret-to-artifact`
