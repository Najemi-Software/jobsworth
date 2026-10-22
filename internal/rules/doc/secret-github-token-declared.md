# secret-github-token-declared

## What it checks

`GITHUB_TOKEN` is declared under `on.workflow_call.secrets`.

## Why it matters

It is always available to the callee; declaring it is noise and callers
cannot pass it explicitly anyway. A note.

## Example: flagged

```yaml
name: Example
on:
  workflow_call:
    secrets:
      GITHUB_TOKEN:
        required: true
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: x=${{ secrets.GITHUB_TOKEN }}
```

## Example: passes

```yaml
name: Example
on:
  workflow_call:
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: x=${{ secrets.GITHUB_TOKEN }}
```

## When it does not fire

Any other secret name.

## Suppress

`# jobsworth-ignore secret-github-token-declared` on the declaration's
line, or `rules: { secret-github-token-declared: off }` in `.jobsworth.yml`.

## Related

`secret-unused`.
