# needs-unknown

## What it checks

A job's `needs:` names a job that does not exist in the file.

## Why it matters

GitHub rejects the workflow at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    needs: biuld
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - run: echo ok
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
```

## When it does not fire

Forward references are allowed: GitHub resolves the whole graph before
running anything, so declaration order is irrelevant.

## Suppress

`# jobsworth-ignore needs-unknown` on the `needs:` line, or
`rules: { needs-unknown: off }` in `.jobsworth.yml`.

## Related

`needs-self`, `needs-cycle`.
