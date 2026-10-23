# call-output-undeclared

## What it checks

`on.workflow_call.outputs.<x>.value` references `jobs.<job>.outputs.<name>`
but `<job>` declares no output `<name>`.

## Why it matters

Every caller receives an empty output; the mismatch is usually a rename
on one side only.

## Example: flagged

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

The job is a `uses:` job whose callee could not be loaded.

## Suppress

`# jobsworth-ignore call-output-undeclared` on the output's line, or
`rules: { call-output-undeclared: off }` in `.jobsworth.yml`.

## Related

`call-output-job-unknown`.
