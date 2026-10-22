# suppress-wildcard

## What it checks

A `jobsworth-ignore *` suppression.

## Why it matters

It hides every rule on that line, including rules added in future
versions, so a new real problem would go unnoticed.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    needs: generated  # jobsworth-ignore *
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
    needs: generated  # jobsworth-ignore needs-unknown
    steps:
      - run: echo ok
```

## When it does not fire

Named rules in a suppression are fine, however many.

## Suppress

`rules: { suppress-wildcard: off }` in `.jobsworth.yml`. Meta-rules cannot
be suppressed inline.

## Related

`suppress-unused`.
