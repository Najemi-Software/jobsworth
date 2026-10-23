# value-set-mismatch

## What it checks

An equality comparison between a path with a documented value set (`steps.*.outcome`, `needs.*.result`, `job.status`,
`runner.os`,
`runner.arch`, `github.ref_type`, `github.event_name`) and a string
literal outside that set.

## Why it matters

Almost always a typo (`sucess`) or a case slip (`linux`); the branch can
never be taken. For `github.event_name` the set is the file's own `on:`
list and the finding is a warning: the branch is dead, not broken.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: t
        run: make test
      - if: steps.t.outcome == 'sucess' && runner.os == 'linux'
        run: echo ok
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: t
        run: make test
      - if: steps.t.outcome == 'success' && runner.os == 'Linux'
        run: echo ok
```

## When it does not fire

Comparisons against expressions or variables. The empty string is
accepted for `outcome`, `conclusion`, `result` and `job.status`: it is
what a step or job that was never reached reads as. `github.event_name`
is only bounded by the file's own triggers: inside a reusable workflow
`github.*` is the caller's context, so `workflow_call` never counts,
and a `workflow_call`-only workflow is not checked.

## Suppress

`# jobsworth-ignore value-set-mismatch` on the expression's line, or
`rules: { value-set-mismatch: off }` in `.jobsworth.yml`.

## Related

`if-always-true-string`.
