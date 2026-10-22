# local-action-no-checkout

## What it checks

A `uses: ./...` step runs in a job with no root checkout before it.

## Why it matters

The workspace is empty until a checkout runs; the runner cannot find
the action and fails the job.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: ./.github/actions/greet
        with:
          name: world
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
      - uses: ./.github/actions/greet
        with:
          name: world
```

## When it does not fire

Composite actions inherit their caller's workspace and are not checked.
Any composite action that calls `actions/checkout` (directly or through another composite action, up to three levels) is
treated as a checkout; its effective `path`, `repository`, `ref` and sparse list come from the wrapper's wiring and the
caller's `with:`. `explain` shows the wiring. When a step's action could not be fetched, or a wrapper computes its
`path` or `repository` dynamically, the job's checkout state is unknown from that step on and this rule stays quiet for
later steps.

## Suppress

`# jobsworth-ignore local-action-no-checkout` on the `uses:` line, or
`rules: { local-action-no-checkout: off }` in `.jobsworth.yml`.

## Related

`local-action-after-subdir-only`.
