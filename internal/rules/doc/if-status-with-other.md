# if-status-with-other

## What it checks

An `if:` that combines `always()` with other conditions using `&&`.

## Why it matters

`always()` is true even when the run was cancelled, so the step still
runs on cancel. `!cancelled()` expresses "run after failure but not after
cancel".

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make
      - if: always() && github.ref == 'refs/heads/main'
        run: echo report
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make
      - if: "!cancelled() && github.ref == 'refs/heads/main'"
        run: echo report
```

## When it does not fire

A condition that already tests `cancelled()` in any polarity (`always() && !cancelled() && ...`), which is the remedy
the rule asks
for. `always()` on its own, or `always()` combined only with status checks,
including `||` groups and negations of them
(`always() && (needs.x.result == 'success' || needs.x.result == 'skipped')`),
(`always() && needs.x.result == 'success'`,
`always() && steps.t.outcome != 'skipped'`): that is the documented
idiom for "run after failures, but require x", and the status check
already excludes the cancelled case.

## Suppress

`# jobsworth-ignore if-status-with-other` on the `if:` line, or
`rules: { if-status-with-other: off }` in `.jobsworth.yml`.

## Related

`if-double-wrapped`.
