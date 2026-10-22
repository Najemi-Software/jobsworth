# output-unused

## What it checks

A job declares an output that no other job references through `needs`
and that is not lifted into `on.workflow_call.outputs`.

## Why it matters

Dead declarations hide which outputs matter. Not a runtime problem.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: "1"
      sha: abc
    steps:
      - run: echo ok
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.version }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    outputs:
      sha:
        value: ${{ jobs.build.outputs.sha }}
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: "1"
      sha: abc
    steps:
      - run: echo ok
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.version }}
```

## When it does not fire

`on.workflow_call.outputs` are consumed by callers in other files and are
never reported. Outputs of `uses:` jobs belong to the callee.

## Suppress

`# jobsworth-ignore output-unused` on the output's line, or
`rules: { output-unused: off }` in `.jobsworth.yml`.

## Related

`needs-output-undeclared`.
