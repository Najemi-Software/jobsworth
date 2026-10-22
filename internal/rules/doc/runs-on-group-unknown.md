# runs-on-group-unknown

<!-- jobsworth-test: enrich self=example/app -->

## What it checks

`runs-on: {group: X}` where the organisation has no runner group `X`. Enrich-only.

## Why it matters

The job never schedules.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on:
      group: huge
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on:
      group: big
    steps:
      - run: echo hi
```

## When it does not fire

Groups the organisation has, and a token that cannot list them.

## Suppress

`# jobsworth-ignore runs-on-group-unknown` on the line, or
`rules: { runs-on-group-unknown: off }` in `.jobsworth.yml`.

## Related

`runs-on-unknown`
