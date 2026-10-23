# fetch-not-found

## What it checks

GitHub answered 404 for the repo, the ref, the workflow file, or the
`action.yml`/`action.yaml` an external `uses:` points at.

## Why it matters

The reference is wrong; the job will fail at runtime.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: acme/nope@v1
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
```

## When it does not fire

Local and linked references that are missing on disk are `uses-not-found`.

## Suppress

`# jobsworth-ignore fetch-not-found` on the `uses:` line, or
`rules: { fetch-not-found: off }` in `.jobsworth.yml`.

## Related

`fetch-failed`, `uses-not-found`.
