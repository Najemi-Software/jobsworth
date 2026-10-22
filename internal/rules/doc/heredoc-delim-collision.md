# heredoc-delim-collision

## What it checks

A multi-line value uses a common delimiter (`EOF`, `END`, `STOP`,
`EOM`, `MARKER`, `_`) and at least one line of the value comes from an
opaque command such as `git`, `jq`, `curl` or `python`.

## Why it matters

If the command's output contains the delimiter on its own line, the
value is cut short and the rest is parsed as new keys. A random
delimiter (`$(openssl rand -hex 8)`) cannot collide.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: log
        run: |
          echo "body<<EOF" >> "$GITHUB_OUTPUT"
          echo "$(git log -5)" >> "$GITHUB_OUTPUT"
          echo "EOF" >> "$GITHUB_OUTPUT"
      - run: echo "${{ steps.log.outputs.body }}"
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: log
        run: |
          delim="jw$(openssl rand -hex 8)"
          echo "body<<$delim" >> "$GITHUB_OUTPUT"
          echo "$(git log -5)" >> "$GITHUB_OUTPUT"
          echo "$delim" >> "$GITHUB_OUTPUT"
      - run: echo "${{ steps.log.outputs.body }}"
```

## When it does not fire

Literal value lines, or an uncommon delimiter. `bash.opaque_commands` in
`.jobsworth.yml` extends the opaque list.

## Suppress

`# jobsworth-ignore heredoc-delim-collision` on the `run:` line, or
`rules: { heredoc-delim-collision: off }` in `.jobsworth.yml`.

## Related

`heredoc-unterminated`.
