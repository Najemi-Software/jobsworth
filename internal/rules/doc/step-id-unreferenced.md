# step-id-unreferenced

<!-- jobsworth-test: enable -->

## What it checks

A step has an `id:` but nothing in the scope references `steps.<id>` at
all.

## Why it matters

Hygiene only. Off by default; ids are often kept for readability.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: build
        run: make
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: build
        run: make
      - if: steps.build.outcome == 'success'
        run: echo ok
```

## When it does not fire

Any reference, including `outcome` and `conclusion`, counts.

## Suppress

Leave the rule off, or `# jobsworth-ignore step-id-unreferenced` on the
id's line.

## Related

`step-output-unused`.
