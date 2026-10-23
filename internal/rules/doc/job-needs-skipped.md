# job-needs-skipped

## What it checks

Job B needs job A, A has an `if:` that can be false, and B's `if:` has no `always()`, `!cancelled()` or `needs.A.result`
check. Not reported when B reads `needs.A.outputs` anywhere: a job that needs A's outputs cannot usefully run when A
skipped, so the silent skip is the intent (the gated-chain idiom).

## Why it matters

When A is skipped, B is skipped silently. Off when A's condition is provably true.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  gate:
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    steps:
      - run: echo gate
  build:
    runs-on: ubuntu-latest
    needs: gate
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  gate:
    runs-on: ubuntu-latest
    if: github.ref == 'refs/heads/main'
    steps:
      - run: echo gate
  build:
    runs-on: ubuntu-latest
    needs: gate
    if: always() && needs.gate.result != 'failure'
    steps:
      - run: echo hi
```

## When it does not fire

Dependencies without an `if:`, or dependants that handle the skip.

## Suppress

`# jobsworth-ignore job-needs-skipped` on the line, or
`rules: { job-needs-skipped: off }` in `.jobsworth.yml`.

## Related

`job-unreachable`
