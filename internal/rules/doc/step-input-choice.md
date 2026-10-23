# step-input-choice

## What it checks

An input declares `type: choice` and the step's literal is not one of
its `options:`.

## Why it matters

The value is rejected at runtime.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/typed
        with:
          count: 1
          level: medium
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
      - uses: ./.github/actions/typed
        with:
          count: 1
          level: high
```

## When it does not fire

Expression values are not checked.

## Suppress

`# jobsworth-ignore step-input-choice` on the `with` value's line, or
`rules: { step-input-choice: off }` in `.jobsworth.yml`.

## Related

`step-input-type`.
