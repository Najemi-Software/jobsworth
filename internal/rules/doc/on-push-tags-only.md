# on-push-tags-only

<!-- jobsworth-test: enable -->

## What it checks

`push` with `tags:` and no `branches:`.

## Why it matters

A common surprise: once `tags:` is present, pushes to branches no longer trigger the workflow unless `branches:` is also
given. Off by default.

## Example: flagged

```yaml
name: Example
on:
  push:
    tags: ['v*']
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on:
  push:
    tags: ['v*']
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

`push` with both or neither.

## Suppress

`# jobsworth-ignore on-push-tags-only` on the line, or
`rules: { on-push-tags-only: off }` in `.jobsworth.yml`.

## Related

None.
