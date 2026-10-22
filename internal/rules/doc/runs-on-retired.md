# runs-on-retired

## What it checks

A `runs-on` label GitHub has retired (`ubuntu-20.04`, `windows-2019`, `macos-12`, ...).

## Why it matters

Retired images never schedule; the message says when GitHub removed it.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-20.04
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

Current labels.

## Suppress

`# jobsworth-ignore runs-on-retired` on the line, or
`rules: { runs-on-retired: off }` in `.jobsworth.yml`.

## Related

`runs-on-unknown`
