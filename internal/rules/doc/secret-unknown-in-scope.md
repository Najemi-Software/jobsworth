# secret-unknown-in-scope

<!-- jobsworth-test: enrich self=example/app -->

## What it checks

`secrets.X` referenced in an entry workflow where no repository or organisation secret named `X` exists. Enrich-only.

## Why it matters

The value is empty at runtime; the usual cause is a secret renamed in settings.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: x=${{ secrets.MISSING_TOKEN }}
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: x=${{ secrets.NPM_TOKEN }}
```

## When it does not fire

Reusable workflows (their secrets come from callers), `GITHUB_TOKEN`, and names the repository, organisation or the
job's `environment:` lists. When any of those lists cannot be read with the token (the organisation list needs
`admin:org`), the scope is unknown and the rule stays quiet rather than guessing.

## Suppress

`# jobsworth-ignore secret-unknown-in-scope` on the line, or
`rules: { secret-unknown-in-scope: off }` in `.jobsworth.yml`.

## Related

`secret-undeclared`
