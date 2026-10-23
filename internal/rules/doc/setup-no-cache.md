# setup-no-cache

<!-- jobsworth-test: enable -->

## What it checks

`actions/setup-node`, `setup-python`, `setup-go` or `setup-java` without `cache:` in a job that runs a package install.

## Why it matters

The built-in cache saves the install on every run. Off by default.

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
      - uses: actions/setup-node@v4
      - run: npm ci
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
      - uses: actions/setup-node@v4
        with:
          cache: npm
      - run: npm ci
```

## When it does not fire

Jobs with no install step, or `cache:` set.

## Suppress

`# jobsworth-ignore setup-no-cache` on the line, or
`rules: { setup-no-cache: off }` in `.jobsworth.yml`.

## Related

None.
