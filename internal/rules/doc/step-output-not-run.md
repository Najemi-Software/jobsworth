# step-output-not-run

## What it checks

`steps.<id>.outputs.<name>` references a step that has neither `run:`
nor `uses:`.

## Why it matters

The step is malformed and GitHub rejects the workflow; the reference can
never resolve.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        name: just a name
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

Every step with `run:` or `uses:`.

## Suppress

`# jobsworth-ignore step-output-not-run` on the referencing line, or
`rules: { step-output-not-run: off }` in `.jobsworth.yml`.

## Related

`step-run-and-uses`.
