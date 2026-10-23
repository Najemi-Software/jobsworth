# step-output-unwritten

## What it checks

A `${{ steps.<id>.outputs.<name> }}` reference points at a `run:` step
whose script never writes `<name>` to `$GITHUB_OUTPUT`.

## Why it matters

The expression evaluates to an empty string at runtime. Nothing fails;
the empty value flows into whatever consumed it, usually a `with:` or
an `if:`, and the symptom shows up somewhere else.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "version=1.2.3"
      - run: echo ${{ steps.meta.outputs.version }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: meta
        run: echo "version=1.2.3" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.meta.outputs.version }}
```

## When it does not fire

The step's `$GITHUB_OUTPUT` target is open (dynamic key, external
script, interpreter), the step's shell is not bash, or the script
failed to parse (`bash-syntax` fires instead). A package runner such as
`uv run`, `npx`, `make` or `poetry run` opens the target the same way
an interpreter does, and so does any command handed a script argument (`x.py`, `x.js`, `x.sh`, ...) or an interpreter
name; extra runners go
in `bash.runner_commands` in `.jobsworth.yml`.

## Suppress

`# jobsworth-ignore step-output-unwritten` on the referencing line, or
`rules: { step-output-unwritten: off }` in `.jobsworth.yml`.

## Related

`bash-output-unredirected` (explains the usual cause),
`step-output-conditional`.
