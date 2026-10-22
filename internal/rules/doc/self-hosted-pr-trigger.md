# self-hosted-pr-trigger

## What it checks

`runs-on` includes `self-hosted` (or a configured self-hosted label) in a workflow triggered by `pull_request` with no
`branches:` restriction.

## Why it matters

Pull requests from forks execute on the self-hosted machine.

## Example: flagged

```yaml
name: Example
on: pull_request
permissions: read-all
jobs:
  build:
    runs-on: [self-hosted, linux]
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: pull_request
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

`pull_request_target`, restricted branches, hosted runners.

## Suppress

`# jobsworth-ignore self-hosted-pr-trigger` on the line, or
`rules: { self-hosted-pr-trigger: off }` in `.jobsworth.yml`.

## Related

None.
