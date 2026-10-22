# uses-not-found

## What it checks

A local `./` reference, or an external reference redirected to a linked
checkout with `--repo`, does not exist on disk.

## Why it matters

For a local reference the job fails at runtime. For a linked one the
checkout and the reference disagree, which is exactly what the link
exists to catch; it is never silently re-fetched from GitHub.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/missing
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/greet
        with:
          name: world
```

## When it does not fire

404s from GitHub are `fetch-not-found`.

## Suppress

`# jobsworth-ignore uses-not-found` on the `uses:` line, or
`rules: { uses-not-found: off }` in `.jobsworth.yml`.

## Related

`fetch-not-found`.
