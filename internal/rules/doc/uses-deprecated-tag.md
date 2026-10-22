# uses-deprecated-tag

<!-- jobsworth-test: enrich self=example/app -->

## What it checks

A `uses:` tag more than one major behind the action's latest release. Enrich-only.

## Why it matters

Old majors stop receiving fixes; the message says how far behind the tag is.

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
      - run: ls -la
```

## When it does not fire

Tags within one major of the latest release, repositories with no releases, SHA-pinned references, and when
`enrich.actions: false` turns the per-action lookups off.

## Suppress

`# jobsworth-ignore uses-deprecated-tag` on the line, or
`rules: { uses-deprecated-tag: off }` in `.jobsworth.yml`.

## Related

`uses-archived`, `uses-unpinned`
