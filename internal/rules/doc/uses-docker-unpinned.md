# uses-docker-unpinned

## What it checks

`docker://image:tag` without `@sha256:`.

## Why it matters

The tag can be re-pointed; the digest cannot.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: docker://alpine:3.19
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
      - uses: docker://alpine@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef
```

## When it does not fire

Digest-pinned images.

## Suppress

`# jobsworth-ignore uses-docker-unpinned` on the line, or
`rules: { uses-docker-unpinned: off }` in `.jobsworth.yml`.

## Related

`uses-unpinned`
