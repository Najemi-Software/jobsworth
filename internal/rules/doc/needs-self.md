# needs-self

## What it checks

A job lists itself in its own `needs:`.

## Why it matters

GitHub rejects the workflow at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    needs: [build]
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
  test:
    runs-on: ubuntu-latest
    needs: [build]
    steps:
      - run: echo ok
```

## When it does not fire

A longer cycle through other jobs is `needs-cycle`, not this rule.

## Suppress

`# jobsworth-ignore needs-self` on the `needs:` line, or
`rules: { needs-self: off }` in `.jobsworth.yml`.

## Related

`needs-cycle`.
