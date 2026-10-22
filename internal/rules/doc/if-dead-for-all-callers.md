# if-dead-for-all-callers

## What it checks

In a reusable workflow or composite action, an `if:` that is false under every combination of values any known caller
passes. Only when every referenced input's value set is complete: no caller passes an expression and no caller is
unknown. `check --input k=v` seeds an entry's own `workflow_dispatch` inputs the same way.

## Why it matters

The step or job never runs for anyone who calls it today. The message lists the callers and their values.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  call:
    uses: ./.github/workflows/gated.yml
    with:
      env: staging
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  call:
    uses: ./.github/workflows/gated.yml
    with:
      env: prod
```

## When it does not fire

Incomplete value sets (an expression-valued or unknown caller), and conditions some caller satisfies.

## Suppress

`# jobsworth-ignore if-dead-for-all-callers` on the line, or
`rules: { if-dead-for-all-callers: off }` in `.jobsworth.yml`.

## Related

`if-dead-for-caller`
