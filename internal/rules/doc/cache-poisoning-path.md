# cache-poisoning-path

## What it checks

On a privileged trigger, a cache restore (`actions/cache`, `setup-*` with `cache:`) whose key includes an untrusted
path.

## Why it matters

A pull request can poison the cache that later privileged runs restore.

## Example: flagged

```yaml
name: Example
on: pull_request_target
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    permissions: read-all
    steps:
      - uses: actions/cache@v4
        with:
          path: ~/.npm
          key: ${{ github.head_ref }}-npm
```

## Example: passes

```yaml
name: Example
on: pull_request_target
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    permissions: read-all
    steps:
      - uses: actions/cache@v4
        with:
          path: ~/.npm
          key: ${{ runner.os }}-npm-${{ hashFiles('**/package-lock.json') }}
```

## When it does not fire

Keys built from `runner.*`, `hashFiles` and the base ref.

## Suppress

`# jobsworth-ignore cache-poisoning-path` on the line, or
`rules: { cache-poisoning-path: off }` in `.jobsworth.yml`.

## Related

`prt-untrusted-checkout`
