# if-redundant-success

<!-- jobsworth-test: enable -->

## What it checks

`if: success()` alone.

## Why it matters

It is the default; the condition adds nothing. Off by default.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - if: success()
        run: echo ok
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
      - run: echo ok
```

## When it does not fire

Any other condition.

## Suppress

`# jobsworth-ignore if-redundant-success` on the line, or
`rules: { if-redundant-success: off }` in `.jobsworth.yml`.

## Related

None.
