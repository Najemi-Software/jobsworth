# checkout-persist-credentials

<!-- jobsworth-test: enable -->

## What it checks

`actions/checkout` without `persist-credentials: false` in a job that later uploads an artifact, saves a cache, or runs
an untrusted action. Off by default; on under `security.strict: true`.

## Why it matters

The token sits in `.git/config` in the working tree.

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
      - uses: actions/upload-artifact@v4
        with:
          path: .
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
        with:
          persist-credentials: false
      - uses: actions/upload-artifact@v4
        with:
          path: dist
```

## When it does not fire

Checkouts with `persist-credentials: false`, or no later exposure.

## Suppress

`# jobsworth-ignore checkout-persist-credentials` on the line, or
`rules: { checkout-persist-credentials: off }` in `.jobsworth.yml`.

## Related

`secret-to-artifact`
