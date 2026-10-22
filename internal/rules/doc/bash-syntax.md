# bash-syntax

## What it checks

A `run:` step whose effective shell is bash or sh does not parse.

## Why it matters

The step fails at runtime, and every other bash rule skips the step, so
nothing else about it can be verified. The message gives the parser's
line and column within the script.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: |
          if [ -f x ]; then
            echo yes
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: |
          if [ -f x ]; then
            echo yes
          fi
```

## When it does not fire

Steps with a non-bash `shell:` (pwsh, python, ...) are not parsed.

## Suppress

`# jobsworth-ignore bash-syntax` on the `run:` line, or
`rules: { bash-syntax: off }` in `.jobsworth.yml`.

## Related

`composite-shell-missing`.
