# uses-unpinned

## What it checks

An external `uses:` from an owner outside the trusted set whose ref is not a 40-hex commit SHA. Trusted: `actions`,
`github`, the entry repository's owner, and `security.trusted_owners`. Short
SHAs do not count as pinned.

## Why it matters

A tag can be moved to malicious code; a SHA cannot.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: someone/thing@v1
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
      - run: make test
```

## When it does not fire

Trusted owners and SHA-pinned references.

## Suppress

`# jobsworth-ignore uses-unpinned` on the line, or
`rules: { uses-unpinned: off }` in `.jobsworth.yml`.

## Related

`uses-unpinned-no-comment`, `uses-docker-unpinned`
