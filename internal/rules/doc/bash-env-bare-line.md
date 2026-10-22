# bash-env-bare-line

## What it checks

A line without `=` is written to `$GITHUB_ENV` outside a `KEY<<DELIM`
value block.

## Why it matters

The runner rejects the line. Either it was meant for `$GITHUB_PATH` or
the key is misspelled.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "/usr/local/bin" >> "$GITHUB_ENV"
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "/usr/local/bin" >> "$GITHUB_PATH"
```

## When it does not fire

Lines inside a heredoc-style value and lines containing expansions.

## Suppress

`# jobsworth-ignore bash-env-bare-line` on the `run:` line, or
`rules: { bash-env-bare-line: off }` in `.jobsworth.yml`.

## Related

`bash-path-kv-line`, `bash-env-bad-key`.
