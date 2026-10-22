# call-input-missing

## What it checks

The callee declares an input with `required: true` and no `default:`, and
the calling job does not supply it in `with:`.

## Why it matters

GitHub rejects the caller at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    with:
      count: 2
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
      count: 2
```

## When it does not fire

The input has a `default:`, or the callee could not be loaded.

## Suppress

`# jobsworth-ignore call-input-missing` on the job's `uses:` line, or
`rules: { call-input-missing: off }` in `.jobsworth.yml`.

## Related

`call-input-unknown`.
