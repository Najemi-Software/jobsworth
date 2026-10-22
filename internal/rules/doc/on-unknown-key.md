# on-unknown-key

## What it checks

A child key of an event that is neither a filter the event supports, `types:`, nor one of its structural keys
(`workflow_run.workflows`, `workflow_dispatch.inputs`, `workflow_call.inputs/outputs/secrets`).

## Why it matters

GitHub rejects the workflow at load time, or ignores the key and runs on every branch.

## Example: flagged

```yaml
name: Example
on:
  push:
    branch: [main]
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
  push:
    branches: [main]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Keys the event documents.

## Suppress

`# jobsworth-ignore on-unknown-key` on the line, or
`rules: { on-unknown-key: off }` in `.jobsworth.yml`.

## Related

`on-unknown-event`
