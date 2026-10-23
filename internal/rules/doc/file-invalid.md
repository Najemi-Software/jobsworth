# file-invalid

## What it checks

An entry file cannot be read, is not valid YAML, or has neither a
top-level `jobs:` (workflow) nor `runs.using` (action).

## Why it matters

Nothing in the file can be validated.

## Example: flagged

```yaml
name: Not a workflow
description: this file has no jobs and no runs block
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
```

## When it does not fire

Files matched by a glob are filtered to workflows and actions before they
become entries; only literal paths reach this rule.

## Suppress

`rules: { file-invalid: off }` in `.jobsworth.yml`. The file has no
parseable lines for an inline suppression.

## Related

`callee-unparseable`.
