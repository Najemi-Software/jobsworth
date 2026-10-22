# checkout-ref-unresolvable

<!-- jobsworth-test: enable -->

## What it checks

A checkout step's `ref:` is an expression.

## Why it matters

Nothing downstream can be verified against the checked-out ref. Off by
default: this is common and usually intentional.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.event.pull_request.head.sha }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          ref: main
```

## When it does not fire

Literal refs, or no `ref:` at all.

## Suppress

Leave the rule off, or `# jobsworth-ignore checkout-ref-unresolvable` on
the `uses:` line.

## Related

`local-action-wrong-repo`.
