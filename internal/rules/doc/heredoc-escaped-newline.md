# heredoc-escaped-newline

## What it checks

`echo "KEY<<EOF\nvalue\nEOF"` without `-e`: the `\n` sequences are
literal backslash-n, so the runner sees one line.

## Why it matters

The value never closes and the step fails; with `-e` the payload is
split into real lines and treated normally.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: notes
        run: echo "body<<EOF\nline\nEOF" >> "$GITHUB_OUTPUT"
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
        run: echo -e "body<<EOF\nline\nEOF" >> "$GITHUB_OUTPUT"
      - run: echo "${{ steps.notes.outputs.body }}"
```

## When it does not fire

`$'\n'` (ANSI-C quoting) yields a real newline and is accepted:
`echo "key<<EOF"$'\n'"$VALUE"$'\n'EOF >> "$GITHUB_OUTPUT"`.

`echo -e` and `printf`, which interpret the escapes.

## Suppress

`# jobsworth-ignore heredoc-escaped-newline` on the `run:` line, or
`rules: { heredoc-escaped-newline: off }` in `.jobsworth.yml`.

## Related

`heredoc-unterminated`.
