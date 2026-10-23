# step-output-conditional

<!-- jobsworth-test: enable -->

## What it checks

A referenced step output is written only on some paths through the
script: inside `if`, `case`, `&&`, `||`, a loop, or after a possible
`exit`.

## Why it matters

The reference may be empty at runtime on the other paths. Off by
default; it is noisy on codebases that write outputs conditionally on
purpose.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: |
          if [ -f VERSION ]; then
            echo "version=$(cat VERSION)" >> "$GITHUB_OUTPUT"
          fi
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
        run: |
          if [ -f VERSION ]; then
            echo "version=$(cat VERSION)" >> "$GITHUB_OUTPUT"
          else
            echo "version=dev" >> "$GITHUB_OUTPUT"
          fi
      - run: echo ${{ steps.meta.outputs.version }}
```

## When it does not fire

Every branch writes the key, or the rule is off (the default).

## Suppress

`# jobsworth-ignore step-output-conditional` on the referencing line, or
leave the rule off.

## Related

`step-output-unwritten`.
