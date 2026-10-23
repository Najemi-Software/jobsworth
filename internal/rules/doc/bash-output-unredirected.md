# bash-output-unredirected

## What it checks

The script has `echo "K=V"` (or a printf equivalent) with no redirect,
`K` is never written to `$GITHUB_OUTPUT` anywhere in the step, and
something in the scope references `steps.<id>.outputs.K`.

## Why it matters

The line goes to the log, not to the output file. `step-output-unwritten`
fires at the same time; this rule explains why.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "version=1"
      - run: echo ${{ steps.meta.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "version=1" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.version }}
```

## When it does not fire

The key is written to `$GITHUB_OUTPUT` elsewhere in the script, or
nothing references it.

## Suppress

`# jobsworth-ignore bash-output-unredirected` on the `run:` line, or
`rules: { bash-output-unredirected: off }` in `.jobsworth.yml`.

## Related

`step-output-unwritten`.
