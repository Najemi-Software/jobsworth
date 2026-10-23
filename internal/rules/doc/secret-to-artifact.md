# secret-to-artifact

## What it checks

A `run:` writes a `secrets.*` value (or an env variable seeded from one) to a file at a literal path, and a later
`actions/upload-artifact` step's `path:` covers it.

## Why it matters

The artifact is downloadable by anyone with read access to the repository.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo "${{ secrets.TOKEN }}" > creds.txt
      - uses: actions/upload-artifact@v4
        with:
          path: creds.txt
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
      - run: echo "${{ secrets.TOKEN }}" > /tmp/creds.txt
      - uses: actions/upload-artifact@v4
        with:
          path: dist
```

## When it does not fire

Files the upload path does not cover.

## Suppress

`# jobsworth-ignore secret-to-artifact` on the line, or
`rules: { secret-to-artifact: off }` in `.jobsworth.yml`.

## Related

`secret-echo`
