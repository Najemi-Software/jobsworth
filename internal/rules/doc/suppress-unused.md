# suppress-unused

## What it checks

A suppression that suppressed nothing.

## Why it matters

Stale comments accumulate and mislead readers about what the line does.
A note, so it never fails the run.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    needs: [test]  # jobsworth-ignore needs-unknown
    steps:
      - run: echo ok
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    needs: generated  # jobsworth-ignore needs-unknown
    steps:
      - run: echo ok
```

## When it does not fire

A suppression that hid at least one finding, even one later turned off by
config.

## Suppress

`rules: { suppress-unused: off }` in `.jobsworth.yml`. Meta-rules cannot
be suppressed inline.

## Related

`suppress-wildcard`.
