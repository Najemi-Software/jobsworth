# if-dead-for-caller

<!-- jobsworth-test: enable -->

## What it checks

An `if:` in a callee that is false for one specific caller's literal values. Noisier than `if-dead-for-all-callers`; off
by default.

## Why it matters

That caller's run skips the step or job.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  staging:
    uses: ./.github/workflows/gated.yml
    with:
      env: staging
  dynamic:
    uses: ./.github/workflows/gated.yml
    with:
      env: ${{ github.ref_name }}
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  prod:
    uses: ./.github/workflows/gated.yml
    with:
      env: prod
  dynamic:
    uses: ./.github/workflows/gated.yml
    with:
      env: ${{ github.ref_name }}
```

## When it does not fire

Callers whose values satisfy the condition.

## Suppress

`# jobsworth-ignore if-dead-for-caller` on the line, or
`rules: { if-dead-for-caller: off }` in `.jobsworth.yml`.

## Related

`if-dead-for-all-callers`
