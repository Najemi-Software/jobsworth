# input-undeclared

## What it checks

An expression references `inputs.<name>` (or `inputs['<name>']`) but no
input called `<name>` is declared under `on.workflow_call.inputs`,
`on.workflow_dispatch.inputs`, or an action's `inputs:`.

## Why it matters

The expression evaluates to an empty string at runtime. Nothing fails at
the reference; the empty value flows into a `with:`, an `if:`, or a
script, and the symptom appears somewhere else.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    inputs:
      env: {type: string}
jobs:
  deploy:
    runs-on: ubuntu-latest
    if: inputs.env == 'prod' && inputs.dry_run
    steps:
      - run: echo ok
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    inputs:
      env: {type: string}
      dry_run: {type: boolean}
jobs:
  deploy:
    runs-on: ubuntu-latest
    if: inputs.env == 'prod' && inputs.dry_run
    steps:
      - run: echo ok
```

## When it does not fire

`github.event.inputs.<name>` is a different context and is not tracked.
References inside a `run:` script body are not expressions unless wrapped
in `${{ }}`.

## Suppress

`# jobsworth-ignore input-undeclared` on the line of the expression, or
`rules: { input-undeclared: off }` in `.jobsworth.yml`.

## Related

`input-unused`.
