# injection-run

## What it checks

An attacker-controlled `github.*` path (issue and PR titles and bodies, comment bodies, commit messages, head refs, ...)
interpolated directly into a `run:` script.

## Why it matters

The text becomes shell code before any quoting applies; `"${{ github.head_ref }}"` is still an injection. Put the value
in `env:` and read the variable in the script.

## Example: flagged

```yaml
name: Example
on: pull_request
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ github.event.pull_request.title }}"
```

## Example: passes

```yaml
name: Example
on: pull_request
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - env:
          TITLE: ${{ github.event.pull_request.title }}
        run: echo "$TITLE"
```

## When it does not fire

The path in `env:`, `if:`, `with:` of a non-sink input, or `name:`; those are the fix.

## Suppress

`# jobsworth-ignore injection-run` on the line, or
`rules: { injection-run: off }` in `.jobsworth.yml`.

## Related

`injection-script`, `injection-via-input`
