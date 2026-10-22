# local-action-sparse-miss

## What it checks

The root checkout has a literal `sparse-checkout` list and no entry
covers the local action's path.

## Why it matters

The action directory is not on disk; the runner fails the step. Cone
semantics: an entry covers itself and everything beneath it.

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
          sparse-checkout: |
            src
      - uses: ./.github/actions/greet
        with:
          name: world
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
          sparse-checkout: |
            src
            .github
      - uses: ./.github/actions/greet
        with:
          name: world
```

## When it does not fire

A list containing an expression, or glob patterns with
`sparse-checkout-cone-mode: false`, disables the rule. A later conditional checkout is a candidate root too: the
reference is fine when any candidate covers it, and the finding names the candidate whose `if:` the consuming step
shares, else the unconditional root.

## Suppress

`# jobsworth-ignore local-action-sparse-miss` on the `uses:` line, or
`rules: { local-action-sparse-miss: off }` in `.jobsworth.yml`.

## Related

`local-action-no-checkout`.
