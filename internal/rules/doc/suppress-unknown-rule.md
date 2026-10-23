# suppress-unknown-rule

## What it checks

A suppression names a rule id that is not registered.

## Why it matters

A typo silently removes the protection the author thought they had
granted; the misspelt id suppresses nothing.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    needs: [test]  # jobsworth-ignore needs-unkown
    steps:
      - run: echo ok
  test:
    runs-on: ubuntu-latest
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

Aliases of renamed rules are accepted.

## Suppress

`rules: { suppress-unknown-rule: off }` in `.jobsworth.yml`. Meta-rules
cannot be suppressed inline.

## Related

`suppress-unused`.
