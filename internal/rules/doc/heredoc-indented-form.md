# heredoc-indented-form

<!-- jobsworth-test: enable -->

## What it checks

A `KEY<<-DELIM` opener written to `$GITHUB_OUTPUT` or `$GITHUB_ENV`.

## Why it matters

The `<<-` form strips leading tabs in bash heredocs, but the runner's
file format is not bash: it treats `<<-DELIM` literally. Off by default
pending confirmation of runner behaviour.

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
          echo "body<<-EOF" >> "$GITHUB_OUTPUT"
          echo "line" >> "$GITHUB_OUTPUT"
          echo "EOF" >> "$GITHUB_OUTPUT"
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

The plain `KEY<<DELIM` form, or the rule left off.

## Suppress

Leave the rule off, or `# jobsworth-ignore heredoc-indented-form` on the
`run:` line.

## Related

`heredoc-unterminated`.
