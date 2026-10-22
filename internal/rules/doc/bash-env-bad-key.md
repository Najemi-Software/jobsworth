# bash-env-bad-key

## What it checks

A `$GITHUB_ENV` line is `=value` (empty key) or its key is not
`[A-Za-z_][A-Za-z0-9_]*`.

## Why it matters

The runner rejects the line and fails the step.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "MY-VAR=1" >> "$GITHUB_ENV"
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "MY_VAR=1" >> "$GITHUB_ENV"
```

## When it does not fire

Dynamic keys are not checked.

## Suppress

`# jobsworth-ignore bash-env-bad-key` on the `run:` line, or
`rules: { bash-env-bad-key: off }` in `.jobsworth.yml`.

## Related

`bash-env-bare-line`.
