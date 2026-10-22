# checkout-redundant

## What it checks

Two checkouts in one job of the same repository and ref, both to the root, where the second's sparse list is a subset of
the first's.

## Why it matters

The second clone replaces an identical tree.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make build
      - uses: actions/checkout@v4
      - run: make test
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
      - run: make build
      - uses: actions/checkout@v4
        with:
          repository: acme/shared
          path: shared
      - run: make test
```

## When it does not fire

Different repositories, refs, paths, or a wider sparse list.

## Suppress

`# jobsworth-ignore checkout-redundant` on the line, or
`rules: { checkout-redundant: off }` in `.jobsworth.yml`.

## Related

`checkout-unused`
