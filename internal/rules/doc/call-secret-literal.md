# call-secret-literal

## What it checks

A value under a call's `secrets:` block is a bare literal rather than an
expression.

## Why it matters

Secret material is committed to source and logged in clear.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/secretive.yml
    secrets:
      token: hunter2
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/secretive.yml
    secrets:
      token: ${{ secrets.TOKEN }}
```

## When it does not fire

Expression values.

## Suppress

`# jobsworth-ignore call-secret-literal` on the secret's line, or
`rules: { call-secret-literal: off }` in `.jobsworth.yml`.

## Related

`call-secret-unknown`.
