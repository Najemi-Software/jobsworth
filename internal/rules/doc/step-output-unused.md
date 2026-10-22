# step-output-unused

## What it checks

A `run:` step with an `id:` writes a key to `$GITHUB_OUTPUT` that nothing
in the scope references as `steps.<id>.outputs.<key>`.

## Why it matters

Dead outputs hide which ones matter and often indicate a consumer that
was renamed. A note, never a failure.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: |
          echo "version=1" >> "$GITHUB_OUTPUT"
          echo "sha=abc" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "version=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.version }}
```

## When it does not fire

Steps without an `id:` (their outputs are unreachable anyway) and steps
whose `$GITHUB_OUTPUT` target is open.

## Suppress

`# jobsworth-ignore step-output-unused` on the `run:` line, or
`rules: { step-output-unused: off }` in `.jobsworth.yml`.

## Related

`step-id-unreferenced`.
