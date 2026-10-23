# step-input-unknown

## What it checks

A `uses:` step supplies `with.X` but the action declares no input `X`.

## Why it matters

The runner warns and ignores the value; the action never sees it.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/greet
        with:
          name: world
          loud: true
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
      - uses: ./.github/actions/greet
        with:
          name: world
```

## When it does not fire

The action is skip-listed, failed to fetch, or is a `docker://`
reference.

## Suppress

`# jobsworth-ignore step-input-unknown` on the `with` value's line, or
`rules: { step-input-unknown: off }` in `.jobsworth.yml`.

## Related

`step-input-missing`.
