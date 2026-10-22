# injection-script

## What it checks

An attacker-controlled path interpolated into the `script:` of `actions/github-script`.

## Why it matters

The text becomes JavaScript. Read it with `core.getInput` or from `env:` instead.

## Example: flagged

```yaml
name: Example
on: issues
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/github-script@v7
        with:
          script: console.log("${{ github.event.issue.title }}")
```

## Example: passes

```yaml
name: Example
on: issues
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/github-script@v7
        env:
          TITLE: ${{ github.event.issue.title }}
        with:
          script: console.log(process.env.TITLE)
```

## When it does not fire

Other inputs of the action.

## Suppress

`# jobsworth-ignore injection-script` on the line, or
`rules: { injection-script: off }` in `.jobsworth.yml`.

## Related

`injection-run`
