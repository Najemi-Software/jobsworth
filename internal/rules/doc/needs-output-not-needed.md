# needs-output-not-needed

## What it checks

A job references `needs.<job>.outputs.<name>` but `<job>` is not in the
referencing job's `needs:`, directly or transitively.

## Why it matters

`needs.<job>` is only populated for dependencies. The expression is empty
at runtime and nothing warns about it.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: "1"
    steps:
      - run: echo ok
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ needs.build.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    outputs:
      version: "1"
    steps:
      - run: echo ok
  deploy:
    runs-on: ubuntu-latest
    needs: build
    steps:
      - run: echo ${{ needs.build.outputs.version }}
```

## When it does not fire

Transitive dependencies are accepted. References outside any job (for
example in `on.workflow_call.outputs`) are checked by the `call-output-*`
rules instead.

## Suppress

`# jobsworth-ignore needs-output-not-needed` on the expression's line, or
`rules: { needs-output-not-needed: off }` in `.jobsworth.yml`.

## Related

`needs-output-undeclared`.
