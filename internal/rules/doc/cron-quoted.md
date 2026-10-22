# cron-quoted

## What it checks

A cron string that is not quoted in YAML.

## Why it matters

`0 * * * *` happens to parse as a string, but `* * * * *` does not; quoting is the convention and avoids the surprise.

## Example: flagged

```yaml
name: Example
on:
  schedule:
    - cron: 0 0 * * *
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on:
  schedule:
    - cron: '0 0 * * *'
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Quoted strings.

## Suppress

`# jobsworth-ignore cron-quoted` on the line, or
`rules: { cron-quoted: off }` in `.jobsworth.yml`.

## Related

`cron-syntax`
