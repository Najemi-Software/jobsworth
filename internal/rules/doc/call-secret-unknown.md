# call-secret-unknown

## What it checks

A reusable-workflow call supplies `secrets.X` but the callee declares no
`X` under `on.workflow_call.secrets`.

## Why it matters

GitHub rejects the caller at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/secretive.yml
    secrets:
      token: ${{ secrets.TOKEN }}
      extra: ${{ secrets.EXTRA }}
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

`secrets:` is an expression, or the callee could not be loaded.

## Suppress

`# jobsworth-ignore call-secret-unknown` on the secret's line, or
`rules: { call-secret-unknown: off }` in `.jobsworth.yml`.

## Related

`call-secret-missing`.
