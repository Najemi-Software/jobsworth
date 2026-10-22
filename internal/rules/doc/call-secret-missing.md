# call-secret-missing

## What it checks

The callee declares a secret with `required: true`, and the caller
supplies neither it nor `secrets: inherit`.

## Why it matters

GitHub rejects the caller at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/secretive.yml
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

Optional secrets, or `secrets: inherit`.

## Suppress

`# jobsworth-ignore call-secret-missing` on the job's `uses:` line, or
`rules: { call-secret-missing: off }` in `.jobsworth.yml`.

## Related

`call-secret-unknown`.
