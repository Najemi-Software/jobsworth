# on-missing

## What it checks

A workflow (a file with top-level `jobs:`) that has no `on:` block.

## Why it matters

It never runs; GitHub lists it but nothing triggers it.

## Example: flagged

```yaml
name: Example
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Composite actions, which have no triggers.

## Suppress

`# jobsworth-ignore on-missing` on the line, or
`rules: { on-missing: off }` in `.jobsworth.yml`.

## Related

`on-unknown-event`
