# callee-unparseable

## What it checks

A file reached through `uses:` is not valid YAML (reported against the
callee) or is neither a workflow nor an action (reported against the
caller).

## Why it matters

The job fails at runtime, and nothing inside the callee can be checked.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/broken.yml
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

Problems with the entry file itself are `file-invalid`.

## Suppress

`rules: { callee-unparseable: off }` in `.jobsworth.yml`. A finding
attributed to an unparseable callee cannot be suppressed inline, because
the callee has no parseable lines.

## Related

`file-invalid`.
