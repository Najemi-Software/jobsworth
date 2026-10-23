# uses-dynamic

## What it checks

A `uses:` contains `${{ }}` and so cannot be resolved statically.

## Why it matters

Nothing about the callee can be verified. GitHub itself does not allow
expressions in `uses:`, so this usually indicates a template that is
rendered before use.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    strategy:
      matrix:
        action: [actions/checkout@v4]
    steps:
      - uses: ${{ matrix.action }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
```

## When it does not fire

Expressions in `with:` values are fine; only the reference itself counts.

## Suppress

`# jobsworth-ignore uses-dynamic` on the `uses:` line, or
`rules: { uses-dynamic: off }` in `.jobsworth.yml`.

## Related

None.
