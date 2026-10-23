# bash-deprecated-command

## What it checks

The script prints `::set-output`, `::save-state`, `::set-env` or
`::add-path` to stdout.

## Why it matters

The runner has ignored these commands since 2023; the output, state,
variable or path is silently dropped. Write to `$GITHUB_OUTPUT`,
`$GITHUB_STATE`, `$GITHUB_ENV` or `$GITHUB_PATH` instead.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "::set-output name=version::1"
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

Inside interpreter inline code, and when the echo is redirected to a
file.

## Suppress

`# jobsworth-ignore bash-deprecated-command` on the `run:` line, or
`rules: { bash-deprecated-command: off }` in `.jobsworth.yml`.

## Related

`bash-output-unredirected`.
