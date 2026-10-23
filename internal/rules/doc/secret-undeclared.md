# secret-undeclared

## What it checks

A reusable workflow references `secrets.X` without declaring `X` under
`on.workflow_call.secrets`.

## Why it matters

Callers cannot pass it, so it is empty at runtime. When a known caller
passes `secrets: inherit` this is a warning; when the workflow also has
a trigger other than `workflow_call` it is skipped, since it may run
directly with repository secrets.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    secrets:
      token:
        required: true
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: x=${{ secrets.token }} ${{ secrets.OTHER }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
    secrets:
      token:
        required: true
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: x=${{ secrets.token }} ${{ secrets.GITHUB_TOKEN }}
```

## When it does not fire

`secrets.GITHUB_TOKEN` is always available. The finding is a warning,
not an error, when a known caller passes `secrets: inherit`, or when
no caller at all is known in this repository: then the contract cannot
be verified from here.

## Suppress

`# jobsworth-ignore secret-undeclared` on the expression's line, or
`rules: { secret-undeclared: off }` in `.jobsworth.yml`.

## Related

`secret-unused`.
