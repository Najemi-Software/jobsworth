# on-unknown-event

## What it checks

An event name under `on:` that is not in the GitHub Actions event table. `triggers.extra_events` in config adds names.

## Why it matters

A misspelled trigger silently changes when the workflow runs; GitHub does not reject it. The message gives the nearest
real name.

## Example: flagged

```yaml
name: Example
on: pushh
jobs:
  build:
    runs-on: ubuntu-latest
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

Events in the table or in `triggers.extra_events`.

## Suppress

`# jobsworth-ignore on-unknown-event` on the line, or
`rules: { on-unknown-event: off }` in `.jobsworth.yml`.

## Related

`on-unknown-key`, `on-unknown-type`
