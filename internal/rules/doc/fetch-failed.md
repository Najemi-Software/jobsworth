# fetch-failed

## What it checks

An external `uses:` could not be fetched from GitHub for a reason other
than "does not exist": no access (401/403), rate limit, network error, or
a server error that persisted after one retry.

## Why it matters

The callee could not be verified, so caller-side checks that need its
declarations were skipped. The reference may still be fine.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: acme/forbidden@v1
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: acme/flaky@v1
```

## When it does not fire

A 404 is `fetch-not-found`; an offline cache miss is `fetch-offline`;
skip-listed references are never fetched.

## Suppress

`# jobsworth-ignore fetch-failed` on the `uses:` line, or
`rules: { fetch-failed: off }` in `.jobsworth.yml` for air-gapped runs.

## Related

`fetch-not-found`, `fetch-offline`.
