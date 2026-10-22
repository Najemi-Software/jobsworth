# artifact-whole-workspace

## What it checks

`actions/upload-artifact` with `path: .` or `path: ${{ github.workspace }}`.

## Why it matters

The whole tree is uploaded, usually unintended and slow; name the files.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: make build
      - uses: actions/upload-artifact@v4
        with:
          path: .
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
      - uses: actions/checkout@v4
      - run: make build
      - uses: actions/upload-artifact@v4
        with:
          path: dist
```

## When it does not fire

Explicit paths.

## Suppress

`# jobsworth-ignore artifact-whole-workspace` on the line, or
`rules: { artifact-whole-workspace: off }` in `.jobsworth.yml`.

## Related

None.
