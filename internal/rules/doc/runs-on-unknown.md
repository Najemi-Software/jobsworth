# runs-on-unknown

## What it checks

A literal `runs-on` label that is not a GitHub-hosted label, does not start with a known OS prefix (the larger-runner
convention), is not in `structure.runner_labels`, and the list has no `self-hosted` or configured self-hosted label.

## Why it matters

The job never schedules; the run waits until it times out.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubunut-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

Self-hosted lists, groups, and labels from config.

## Suppress

`# jobsworth-ignore runs-on-unknown` on the line, or
`rules: { runs-on-unknown: off }` in `.jobsworth.yml`.

## Related

`runs-on-retired`, `runs-on-matrix`
