# needs-output-job-unknown

## What it checks

An expression references `needs.<job>.outputs.<name>` for a `<job>` that
does not exist in the file.

## Why it matters

The expression evaluates to empty at runtime; usually a renamed job.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ needs.biuld.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: ${{ steps.v.outputs.version }}
    steps:
      - id: v
        run: echo "version=1" >> "$GITHUB_OUTPUT"
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.version }}
```

## When it does not fire

`needs.<job>.result` is not an output and is never checked here.

## Suppress

`# jobsworth-ignore needs-output-job-unknown` on the expression's line, or
`rules: { needs-output-job-unknown: off }` in `.jobsworth.yml`.

## Related

`needs-output-not-needed`, `needs-output-undeclared`.
