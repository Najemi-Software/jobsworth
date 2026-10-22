# job-orphan-outputs

## What it checks

In a `workflow_call` workflow, a job declares `outputs:` that no job needs (transitively) and no
`on.workflow_call.outputs.*.value` lifts, while something still references them. Matrix jobs are skipped, and so is a
job whose every output already carries `output-unused`.

## Why it matters

The outputs are computed and then dropped; either delete the block or lift it into the workflow's outputs. The per-job
message says which.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
jobs:
  a:
    runs-on: ubuntu-latest
    outputs:
      x: ${{ steps.s.outputs.x }}
    steps:
      - id: s
        run: echo "x=1" >> "$GITHUB_OUTPUT"
  b:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ needs.a.outputs.x }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    outputs:
      x:
        value: ${{ jobs.a.outputs.x }}
jobs:
  a:
    runs-on: ubuntu-latest
    outputs:
      x: ${{ steps.s.outputs.x }}
    steps:
      - id: s
        run: echo "x=1" >> "$GITHUB_OUTPUT"
```

## When it does not fire

Jobs some other job needs, or whose outputs the workflow lifts.

## Suppress

`# jobsworth-ignore job-orphan-outputs` on the line, or
`rules: { job-orphan-outputs: off }` in `.jobsworth.yml`.

## Related

`output-unused`, `needs-output-not-needed`
