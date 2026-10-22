# call-secret-inherit-partial

<!-- jobsworth-test: enable -->

## What it checks

`secrets: inherit` on a call to a workflow that declares no secrets.

## Why it matters

Harmless, and off by default; it merely passes more than the callee
can use.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    with:
      env: prod
    secrets: inherit
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/secretive.yml
    secrets: inherit
```

## When it does not fire

The callee declares at least one secret.

## Suppress

Leave the rule off.

## Related

`call-secret-missing`.
