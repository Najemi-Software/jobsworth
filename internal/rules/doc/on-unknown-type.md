# on-unknown-type

## What it checks

A `types:` entry that is not an activity type of the event.

## Why it matters

GitHub ignores it, so the workflow never fires for the type that was meant.

## Example: flagged

```yaml
name: Example
on:
  pull_request:
    types: [opened, synchronise]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on:
  pull_request:
    types: [opened, synchronize]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

`repository_dispatch`, whose types are user-defined.

## Suppress

`# jobsworth-ignore on-unknown-type` on the line, or
`rules: { on-unknown-type: off }` in `.jobsworth.yml`.

## Related

`on-unknown-event`
