# jq-syntax

## What it checks

A `jq` invocation in a `run:` script whose literal filter does not parse.

## Why it matters

The step fails at runtime with jq's own error. The message gives the
position within the filter.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: |
          jq '{version: .tag' release.json
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
          jq '{version: .tag}' release.json
```

## When it does not fire

Filters that are not literal (built from variables) are not parsed.

## Suppress

`# jobsworth-ignore jq-syntax` on the `run:` line, or
`rules: { jq-syntax: off }` in `.jobsworth.yml`.

## Related

`shape-mismatch`.
