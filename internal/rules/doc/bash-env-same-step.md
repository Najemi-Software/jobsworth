# bash-env-same-step

## What it checks

The script writes `K` to `$GITHUB_ENV` and reads `$K` later in the same
script.

## Why it matters

`$GITHUB_ENV` takes effect from the next step; within the script `$K`
still has its old value (usually empty).

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: |
          echo "VERSION=1.2.3" >> "$GITHUB_ENV"
          echo "building $VERSION"
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
          VERSION=1.2.3
          echo "VERSION=$VERSION" >> "$GITHUB_ENV"
          echo "building $VERSION"
```

## When it does not fire

Reading the variable before the write, or in a later step.

## Suppress

`# jobsworth-ignore bash-env-same-step` on the `run:` line, or
`rules: { bash-env-same-step: off }` in `.jobsworth.yml`.

## Related

`bash-output-read`.
