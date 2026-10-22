# checkout-trailing

## What it checks

A checkout as the final step of a job.

## Why it matters

Nothing can read the tree; the clone is wasted.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo before
      - uses: actions/checkout@v4
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
      - uses: actions/checkout@v4
      - run: make test
```

## When it does not fire

Checkouts with steps after them, and checkouts performed by a composite wrapper whose own later steps read the tree.

## Suppress

`# jobsworth-ignore checkout-trailing` on the line, or
`rules: { checkout-trailing: off }` in `.jobsworth.yml`.

## Related

`checkout-unused`
