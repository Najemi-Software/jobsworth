# injection-via-input

## What it checks

A caller passes an attacker-controlled path to an action or reusable-workflow input that the callee interpolates into a
`run:` or `github-script` sink. Attributed to the caller; the message names the input and the callee's sink line.

## Why it matters

The callee cannot know what it will be given; flagging it would punish every well-behaved caller.

## Example: flagged

```yaml
name: Example
on: pull_request
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: ./.github/actions/greet
        with:
          name: ${{ github.event.pull_request.title }}
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
      - uses: actions/checkout@v4
      - uses: ./.github/actions/greet
        with:
          name: ${{ github.actor }}
```

## When it does not fire

Inputs the callee only uses in `if:`, `env:` or `with:`.

## Suppress

`# jobsworth-ignore injection-via-input` on the line, or
`rules: { injection-via-input: off }` in `.jobsworth.yml`.

## Related

`injection-run`
