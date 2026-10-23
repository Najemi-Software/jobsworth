# step-id-invalid

## What it checks

A step `id:` contains characters outside `[A-Za-z0-9_-]` or starts with
a digit.

## Why it matters

GitHub rejects the workflow at parse time, or the id cannot be referenced
with dot syntax.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: 1st.step
        run: echo "a=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps['1st.step'].outputs.a }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: first_step
        run: echo "a=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.first_step.outputs.a }}
```

## When it does not fire

Hyphens and underscores are allowed anywhere after the first character.

## Suppress

`# jobsworth-ignore step-id-invalid` on the id's line, or
`rules: { step-id-invalid: off }` in `.jobsworth.yml`.

## Related

`step-id-duplicate`.
