# runs-on-group-and-labels

<!-- jobsworth-test: enable -->

## What it checks

`runs-on: {group: X}` with no `labels:`.

## Why it matters

Valid, but often a leftover from a copied job. Off by default.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on:
      group: big
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on:
      group: big
      labels: [linux]
    steps:
      - run: echo hi
```

## When it does not fire

Groups with labels.

## Suppress

`# jobsworth-ignore runs-on-group-and-labels` on the line, or
`rules: { runs-on-group-and-labels: off }` in `.jobsworth.yml`.

## Related

None.
