# call-input-unknown

## What it checks

A job with `uses:` pointing at a reusable workflow supplies `with.<name>`
but the callee declares no `<name>` under `on.workflow_call.inputs`.

## Why it matters

GitHub rejects the caller at parse time. A misspelt input name is the
usual cause.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    with:
      env: prod
      evn: staging
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    with:
      env: prod
```

## When it does not fire

The callee could not be loaded (fetch failure, skip list); the fetch
finding covers that. Dispatch-only inputs of the callee do not count as
declared for `workflow_call`.

## Suppress

`# jobsworth-ignore call-input-unknown` on the `with` value's line, or
`rules: { call-input-unknown: off }` in `.jobsworth.yml`.

## Related

`call-input-missing`, `call-input-type`.
