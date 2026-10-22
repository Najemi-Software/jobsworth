# step-input-type

## What it checks

A step passes a literal that does not fit the input's declared `type:`.
Only inputs that carry a `type:` are checked; plain `action.yml` inputs
are untyped strings.

## Why it matters

The value is coerced or rejected at runtime.

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
          count: many
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
          count: 3
```

## When it does not fire

Untyped inputs, or an action that could not be loaded. An expression is
checked only when its inferred type is definite (a comparison, a number,
a JSON value); a string-shaped expression such as
`${{ github.run_number }}` may hold a number and passes.

## Suppress

`# jobsworth-ignore step-input-type` on the `with` value's line, or
`rules: { step-input-type: off }` in `.jobsworth.yml`.

## Related

`step-input-choice`.
