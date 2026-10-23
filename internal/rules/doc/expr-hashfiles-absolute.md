# expr-hashfiles-absolute

## What it checks

`hashFiles()` with a literal pattern that is absolute or escapes the workspace (`/abs/path`, `../x`).

## Why it matters

`hashFiles` only matches files under `GITHUB_WORKSPACE`; such a pattern returns an empty string and every cache key
built from it collides.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ hashFiles('/etc/hosts') }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ hashFiles('**/package-lock.json') }}
```

## When it does not fire

Relative patterns, and patterns that are expressions.

## Suppress

`# jobsworth-ignore expr-hashfiles-absolute` on the line, or
`rules: { expr-hashfiles-absolute: off }` in `.jobsworth.yml`.

## Related

None.
