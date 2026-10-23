# concurrency-cancel-on-push-main

<!-- jobsworth-test: enable -->

## What it checks

`cancel-in-progress: true` with a group that includes `github.ref`, on a workflow that pushes to the default branch and
deploys (a job with `environment:`).

## Why it matters

A second push cancels a half-finished deploy. Heuristic; off by default.

## Example: flagged

```yaml
name: Example
on:
  push:
    branches: [main]
concurrency:
  group: deploy-${{ github.ref }}
  cancel-in-progress: true
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - run: echo deploy
```

## Example: passes

```yaml
name: Example
on:
  push:
    branches: [main]
concurrency:
  group: deploy-${{ github.ref }}
  cancel-in-progress: false
jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - run: echo deploy
```

## When it does not fire

Groups without `github.ref`, workflows that do not deploy, or `cancel-in-progress: false`.

## Suppress

`# jobsworth-ignore concurrency-cancel-on-push-main` on the line, or
`rules: { concurrency-cancel-on-push-main: off }` in `.jobsworth.yml`.

## Related

`concurrency-group-static`
