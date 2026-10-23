# suppress-unclosed

## What it checks

A `jobsworth-ignore-start` with no matching `jobsworth-ignore-end`.

## Why it matters

The region silently runs to the end of the file, hiding far more than
intended.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  # jobsworth-ignore-start needs-unknown
  build:
    runs-on: ubuntu-latest
    needs: generated
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  # jobsworth-ignore-start needs-unknown
  build:
    runs-on: ubuntu-latest
    needs: generated
    steps:
      - run: echo ok
  # jobsworth-ignore-end
```

## When it does not fire

`jobsworth-ignore-file` is intentionally file-wide and never reported.

## Suppress

`rules: { suppress-unclosed: off }` in `.jobsworth.yml`. Meta-rules cannot
be suppressed inline.

## Related

None.
