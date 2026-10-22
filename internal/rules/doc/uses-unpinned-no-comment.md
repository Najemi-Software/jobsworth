# uses-unpinned-no-comment

<!-- jobsworth-test: enable -->

## What it checks

A SHA-pinned `uses:` with no trailing `# vX.Y` comment.

## Why it matters

Readers cannot tell which release the SHA is. Off by default; `pin` writes the comment and some teams enforce it.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567
      - run: make test
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
      - uses: actions/checkout@0123456789abcdef0123456789abcdef01234567 # v4.1.0
      - run: make test
```

## When it does not fire

Comments starting with a version.

## Suppress

`# jobsworth-ignore uses-unpinned-no-comment` on the line, or
`rules: { uses-unpinned-no-comment: off }` in `.jobsworth.yml`.

## Related

`uses-unpinned`
