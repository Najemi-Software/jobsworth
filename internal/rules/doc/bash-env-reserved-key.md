# bash-env-reserved-key

## What it checks

A `$GITHUB_ENV` line sets a key the runner owns: `GITHUB_*`, `RUNNER_*`
or `CI`.

## Why it matters

The runner ignores the write; the variable keeps its runner value.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "GITHUB_SHA=abc" >> "$GITHUB_ENV"
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "APP_SHA=abc" >> "$GITHUB_ENV"
```

## When it does not fire

Any key outside the reserved prefixes.

## Suppress

`# jobsworth-ignore bash-env-reserved-key` on the `run:` line, or
`rules: { bash-env-reserved-key: off }` in `.jobsworth.yml`.

## Related

`bash-env-bad-key`.
