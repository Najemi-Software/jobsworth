# step-output-action-undeclared

## What it checks

`steps.<id>.outputs.<name>` references a `uses:` step whose action
declarations are known, and the action declares no output `<name>`.

## Why it matters

The value is empty at runtime; usually a typo or an output the action
removed.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - id: meta
        uses: ./.github/actions/meta
      - run: echo ${{ steps.meta.outputs.sha }} ${{ steps.meta.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - id: meta
        uses: ./.github/actions/meta
      - run: echo ${{ steps.meta.outputs.version }}
```

## When it does not fire

The action could not be loaded (skip list, fetch failure) or is a
`docker://` reference. Only composite actions are checked: JavaScript
and Docker actions set outputs at runtime (`core.setOutput`, a filter
name in `dorny/paths-filter`), so their declared list is a lower bound
and an undeclared name may well exist.

## Suppress

`# jobsworth-ignore step-output-action-undeclared` on the referencing
line, or `rules: { step-output-action-undeclared: off }` in `.jobsworth.yml`.

## Related

`step-output-unwritten`.
