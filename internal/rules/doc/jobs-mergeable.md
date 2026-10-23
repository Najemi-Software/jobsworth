# jobs-mergeable

## What it checks

Job A whose only consumer is job B, with B needing only A, identical `runs-on`, `container`, `services` and defaults, no
matrix, no `if:` on either, and none of A's outputs lifted by a `workflow_call` output.

## Why it matters

One runner spin-up saved by putting both step lists in one job. Pairwise only.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: make build
  test:
    runs-on: ubuntu-latest
    needs: build
    steps:
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
      - run: make build
  test:
    runs-on: windows-latest
    needs: build
    steps:
      - run: make test
```

## When it does not fire

Different runners, matrices, conditions, several consumers, or a first job that declares outputs (merging would mean
rewriting the output plumbing).

## Suppress

`# jobsworth-ignore jobs-mergeable` on the line, or
`rules: { jobs-mergeable: off }` in `.jobsworth.yml`.

## Related

None.
