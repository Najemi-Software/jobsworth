# call-output-job-unknown

## What it checks

`on.workflow_call.outputs.<x>.value` references `jobs.<job>.outputs.<name>`
for a `<job>` that does not exist.

## Why it matters

Every caller of the reusable workflow receives an empty output.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    outputs:
      version:
        value: ${{ jobs.biuld.outputs.version }}
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    outputs:
      version:
        value: ${{ jobs.build.outputs.version }}
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: "1"
    steps:
      - run: echo ok
```

## When it does not fire

Only `on.workflow_call.outputs` values are inspected for `jobs.*` paths.

## Suppress

`# jobsworth-ignore call-output-job-unknown` on the output's line, or
`rules: { call-output-job-unknown: off }` in `.jobsworth.yml`.

## Related

`call-output-undeclared`.
