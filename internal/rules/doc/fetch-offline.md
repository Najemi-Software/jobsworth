# fetch-offline

<!-- jobsworth-test: offline -->

## What it checks

The run is `--offline` and the external `uses:` is not in the cache.

## Why it matters

The callee could not be verified. Run once online to warm the cache.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: acme/never-fetched@v1
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "nothing to fetch"
```

## When it does not fire

Cache hits are used normally while offline; local and linked references
never touch the network.

## Suppress

`# jobsworth-ignore fetch-offline` on the `uses:` line, or
`rules: { fetch-offline: off }` in `.jobsworth.yml`.

## Related

`fetch-failed`.
