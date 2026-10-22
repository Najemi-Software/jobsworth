# uses-no-ref

## What it checks

An external `uses:` has no `@ref`.

## Why it matters

GitHub rejects the workflow at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout
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

Local (`./`) and `docker://` references take no ref.

## Suppress

`# jobsworth-ignore uses-no-ref` on the `uses:` line, or
`rules: { uses-no-ref: off }` in `.jobsworth.yml`.

## Related

`uses-invalid`.
