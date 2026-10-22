# step-continue-on-error-outputs

## What it checks

`continue-on-error: true` on a step whose outputs a later step consumes without checking `steps.<id>.outcome` or
`.conclusion`.

## Why it matters

When the step fails, the consumer runs with empty values.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: t
        continue-on-error: true
        run: echo "v=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.t.outputs.v }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: t
        continue-on-error: true
        run: echo "v=1" >> "$GITHUB_OUTPUT"
      - if: steps.t.outcome == 'success'
        run: echo ${{ steps.t.outputs.v }}
```

## When it does not fire

Consumers guarded by an outcome check.

## Suppress

`# jobsworth-ignore step-continue-on-error-outputs` on the line, or
`rules: { step-continue-on-error-outputs: off }` in `.jobsworth.yml`.

## Related

`step-output-conditional`
