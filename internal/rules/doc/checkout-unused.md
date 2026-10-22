# checkout-unused

## What it checks

A checkout followed only by filesystem-clean steps: `run:` bodies made of echo, printf, assignments and `$GITHUB_*`
writes with no relative path, `git`, workspace reference or opaque call; `uses:` steps that resolve to a clean composite
action or to a `performance.fs_clean_actions` entry.

## Why it matters

The clone is the slowest part of many small jobs and nothing reads it.

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
      - run: echo "ok=1" >> "$GITHUB_OUTPUT"
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

Any later step that may touch the tree, including the steps inside a composite wrapper after its checkout.

## Suppress

`# jobsworth-ignore checkout-unused` on the line, or
`rules: { checkout-unused: off }` in `.jobsworth.yml`.

## Related

`checkout-trailing`, `checkout-redundant`
