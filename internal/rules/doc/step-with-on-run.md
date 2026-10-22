# step-with-on-run

## What it checks

A `run:` step has a `with:` block.

## Why it matters

The runner ignores `with:` on run steps; the values never reach the
script. Usually a step that was converted from `uses:` to `run:`.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make
        with:
          target: all
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make $TARGET
        env:
          TARGET: all
```

## When it does not fire

`with:` on a `uses:` step.

## Suppress

`# jobsworth-ignore step-with-on-run` on the `with:` line, or
`rules: { step-with-on-run: off }` in `.jobsworth.yml`.

## Related

`step-run-and-uses`.
