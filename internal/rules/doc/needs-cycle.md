# needs-cycle

## What it checks

`needs:` edges form a cycle. Each cycle is reported once, listing the jobs
involved.

## Why it matters

GitHub rejects the workflow at parse time; no job in the cycle can ever
start.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  a:
    runs-on: ubuntu-latest
    needs: b
    steps:
      - run: echo ok
  b:
    runs-on: ubuntu-latest
    needs: a
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  a:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
  b:
    runs-on: ubuntu-latest
    needs: a
    steps:
      - run: echo ok
  c:
    runs-on: ubuntu-latest
    needs: [a, b]
    steps:
      - run: echo ok
```

## When it does not fire

A job needing itself is `needs-self`. Edges to unknown jobs are
`needs-unknown` and are not part of cycle detection.

## Suppress

`# jobsworth-ignore needs-cycle` on the first job's line, or
`rules: { needs-cycle: off }` in `.jobsworth.yml`.

## Related

`needs-self`.
