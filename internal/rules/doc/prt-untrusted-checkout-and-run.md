# prt-untrusted-checkout-and-run

## What it checks

The checkout of `prt-untrusted-checkout`, followed in the same job by a step that runs code from the working tree: a
`./` action, a `run:` invoking a relative path, a package install, `make`, `bundle`, or an opaque script.

## Why it matters

This is the full "pwn request": attacker code executes with the base repository's secrets and a write token. Certain
enough to fail.

## Example: flagged

```yaml
name: Example
on: pull_request_target
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    permissions: read-all
    steps:
      - uses: actions/checkout@v4
        with:
          ref: ${{ github.event.pull_request.head.sha }}
      - run: npm install
```

## Example: passes

```yaml
name: Example
on: pull_request_target
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    permissions: read-all
    steps:
      - uses: actions/checkout@v4
      - run: npm install
```

## When it does not fire

Checkouts of the base ref.

## Suppress

`# jobsworth-ignore prt-untrusted-checkout-and-run` on the line, or
`rules: { prt-untrusted-checkout-and-run: off }` in `.jobsworth.yml`.

## Related

`prt-untrusted-checkout`
