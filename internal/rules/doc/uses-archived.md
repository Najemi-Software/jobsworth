# uses-archived

<!-- jobsworth-test: enrich self=example/app -->

## What it checks

An external action whose repository is archived. Enrich-only.

## Why it matters

An archived action receives no fixes, security or otherwise.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: someone/old@v1
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

Active repositories.

## Suppress

`# jobsworth-ignore uses-archived` on the line, or
`rules: { uses-archived: off }` in `.jobsworth.yml`.

## Related

`uses-deprecated-tag`, `uses-unpinned`
