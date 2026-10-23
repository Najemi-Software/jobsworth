# heredoc-unterminated

## What it checks

A `KEY<<DELIM` opener written to `$GITHUB_OUTPUT` or `$GITHUB_ENV` has no
matching `DELIM` line before the step's writes to that target end.

## Why it matters

The runner fails the step: the multi-line value never closes.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: notes
        run: |
          echo "body<<EOF" >> "$GITHUB_OUTPUT"
          echo "line one" >> "$GITHUB_OUTPUT"
      - run: echo "${{ steps.notes.outputs.body }}"
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: notes
        run: |
          echo "body<<EOF" >> "$GITHUB_OUTPUT"
          echo "line one" >> "$GITHUB_OUTPUT"
          echo "EOF" >> "$GITHUB_OUTPUT"
      - run: echo "${{ steps.notes.outputs.body }}"
```

## When it does not fire

Openers written from a line containing an expression are skipped.

## Suppress

`# jobsworth-ignore heredoc-unterminated` on the `run:` line, or
`rules: { heredoc-unterminated: off }` in `.jobsworth.yml`.

## Related

`heredoc-empty-delim`.
