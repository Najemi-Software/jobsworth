# local-action-after-subdir-only

## What it checks

The only checkouts before a `./` action use `path:`, so the workspace
root is still empty.

## Why it matters

Same failure as no checkout at all; the message points at `path:`.

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
          path: src
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
      - uses: actions/checkout@v4
        with:
          path: other
      - uses: ./.github/actions/greet
        with:
          name: world
```

## When it does not fire

A root checkout anywhere earlier in the job, or a subdirectory checkout
whose `path:` is a prefix of the reference (`path: tools` providing
`./tools/.github/actions/x`): that checkout provides the action, and it
is loaded from the checkout's repository at the path inside it.

## Suppress

`# jobsworth-ignore local-action-after-subdir-only` on the `uses:` line,
or `rules: { local-action-after-subdir-only: off }` in `.jobsworth.yml`.

## Related

`local-action-no-checkout`.
