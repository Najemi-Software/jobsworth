# pwsh-syntax

## What it checks

A `shell: pwsh` step whose body `[scriptblock]::Create` rejects, when `pwsh` is on PATH (`scripts.pwsh`).

## Why it matters

The step fails at runtime.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - shell: pwsh
        run: |
          Write-Host "unterminated
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
      - shell: pwsh
        run: |
          Write-Host "ok"
```

## When it does not fire

Bodies that parse, and an absent interpreter.

## Suppress

`# jobsworth-ignore pwsh-syntax` on the line, or
`rules: { pwsh-syntax: off }` in `.jobsworth.yml`.

## Related

`python-syntax`
