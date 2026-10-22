# step-input-missing

## What it checks

An action declares an input with `required: true` and no `default:`, and
the step does not supply it.

## Why it matters

The runner fails the step before the action runs.

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

Inputs with a `default:`, or an action that could not be loaded.

## Suppress

`# jobsworth-ignore step-input-missing` on the `uses:` line, or
`rules: { step-input-missing: off }` in `.jobsworth.yml`.

## Related

`step-input-unknown`.
