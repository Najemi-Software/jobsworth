# secret-unused

## What it checks

`on.workflow_call.secrets.X` is declared but `secrets.X` is never
referenced.

## Why it matters

Callers keep passing a secret nobody reads. A note.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    secrets:
      token:
        required: true
      unused:
        required: false
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: x=${{ secrets.token }}
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
      - run: x=${{ secrets.token }}
```

## When it does not fire

Every declared secret is referenced somewhere in the file.

## Suppress

`# jobsworth-ignore secret-unused` on the declaration's line, or
`rules: { secret-unused: off }` in `.jobsworth.yml`.

## Related

`secret-undeclared`.
