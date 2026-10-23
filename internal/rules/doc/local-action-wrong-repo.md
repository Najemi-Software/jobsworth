# local-action-wrong-repo

<!-- jobsworth-test: self=acme/app -->

## What it checks

The job's root checkout is of a different repository than the file's
own, and the file's repository does not have the local action path.

## Why it matters

`./` actions are loaded from the workspace, so the action comes from the
checked-out repository, not the one the workflow lives in.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
        with:
          repository: acme/shared
      - uses: ./.github/actions/notify
        with:
          env: prod
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

The file's own repository is unknown (no git origin), or it has the
action path too.

## Suppress

`# jobsworth-ignore local-action-wrong-repo` on the `uses:` line, or
`rules: { local-action-wrong-repo: off }` in `.jobsworth.yml`.

## Related

`local-action-no-checkout`.
