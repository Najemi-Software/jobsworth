# bash-path-kv-line

## What it checks

A `KEY=VALUE` line is written to `$GITHUB_PATH`.

## Why it matters

`$GITHUB_PATH` takes directories, one per line. A `KEY=VALUE` line was
almost certainly meant for `$GITHUB_ENV`.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "GOFLAGS=-mod=vendor" >> "$GITHUB_PATH"
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "$HOME/go/bin" >> "$GITHUB_PATH"
```

## When it does not fire

Lines containing an expression or a shell expansion are skipped.

## Suppress

`# jobsworth-ignore bash-path-kv-line` on the `run:` line, or
`rules: { bash-path-kv-line: off }` in `.jobsworth.yml`.

## Related

`bash-env-bare-line`.
