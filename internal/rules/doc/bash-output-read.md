# bash-output-read

## What it checks

The script reads `$GITHUB_OUTPUT`: as an input redirect, as a file
argument to `cat`, `grep`, `head`, `tail`, `awk`, `sed`, `wc`, `stat`,
`file`, `[`, `test`, `[[ ]]`, or via `source`.

## Why it matters

The file is append-only from the step's point of view and never holds
other steps' outputs; cross-step state is read through
`steps.<id>.outputs.*` expressions.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: cat "$GITHUB_OUTPUT"
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "see $GITHUB_OUTPUT for the format"
```

## When it does not fire

Mid-string interpolation is not a read.

## Suppress

`# jobsworth-ignore bash-output-read` on the `run:` line, or
`rules: { bash-output-read: off }` in `.jobsworth.yml`.

## Related

`bash-env-same-step`.
