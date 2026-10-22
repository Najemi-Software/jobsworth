# permissions-missing

## What it checks

No `permissions:` at workflow level and none on at least one job, in a workflow whose triggers carry a token.

## Why it matters

The token then gets the repository default, which may be read-write. Suppressed under `--enrich` when the repository
default is read-only.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
permissions:
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Reusable workflows (the caller's token applies) and workflows with a block at either level.

## Suppress

`# jobsworth-ignore permissions-missing` on the line, or
`rules: { permissions-missing: off }` in `.jobsworth.yml`.

## Related

`permissions-write-all`, `prt-no-permissions`
