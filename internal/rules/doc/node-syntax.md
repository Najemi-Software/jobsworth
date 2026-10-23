# node-syntax

<!-- jobsworth-test: enable -->

## What it checks

A `shell: node {0}` step that `node --check` rejects. Off by default.

## Why it matters

The step fails at runtime.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - shell: node {0}
        run: |
          function (
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - shell: node {0}
        run: |
          console.log('ok')
```

## When it does not fire

Bodies that parse, and an absent interpreter.

## Suppress

`# jobsworth-ignore node-syntax` on the line, or
`rules: { node-syntax: off }` in `.jobsworth.yml`.

## Related

`ruby-syntax`
