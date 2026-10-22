# python-syntax

## What it checks

A `shell: python` step whose body `ast.parse` rejects, when a python interpreter is on PATH (`scripts.python`).

## Why it matters

The step fails at runtime. A warning, not an error, because the validator's interpreter may differ from the runner's.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - shell: python
        run: |
          def broken(:
              pass
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
      - shell: python
        run: |
          print("ok")
```

## When it does not fire

Bodies that parse, and an absent interpreter.

## Suppress

`# jobsworth-ignore python-syntax` on the line, or
`rules: { python-syntax: off }` in `.jobsworth.yml`.

## Related

`pwsh-syntax`
