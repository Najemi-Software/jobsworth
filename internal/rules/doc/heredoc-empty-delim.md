# heredoc-empty-delim

## What it checks

A `KEY<<` line with nothing after `<<` is written to `$GITHUB_OUTPUT` or
`$GITHUB_ENV`.

## Why it matters

The runner cannot know where the value ends and fails the step.

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
          echo "body<<" >> "$GITHUB_OUTPUT"
          echo "line" >> "$GITHUB_OUTPUT"
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
          echo "line" >> "$GITHUB_OUTPUT"
          echo "EOF" >> "$GITHUB_OUTPUT"
      - run: echo "${{ steps.notes.outputs.body }}"
```

## When it does not fire

Any opener with a delimiter.

## Suppress

`# jobsworth-ignore heredoc-empty-delim` on the `run:` line, or
`rules: { heredoc-empty-delim: off }` in `.jobsworth.yml`.

## Related

`heredoc-unterminated`.
