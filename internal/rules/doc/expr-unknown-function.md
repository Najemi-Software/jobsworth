# expr-unknown-function

## What it checks

A call to a name that is not one of the twelve GitHub Actions expression functions: `contains`, `startsWith`,
`endsWith`, `format`, `join`, `toJSON`, `fromJSON`, `hashFiles`, `success`, `always`, `cancelled`, `failure`. Names are
matched case-insensitively (`fromJson` is fine).

## Why it matters

GitHub rejects the workflow at load time with an expression error; nothing runs. The message names the nearest real
function.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ formatt('{0}', github.sha) }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ format('{0}', github.sha) }}
```

## When it does not fire

Any of the twelve functions in any casing.

## Suppress

`# jobsworth-ignore expr-unknown-function` on the line, or
`rules: { expr-unknown-function: off }` in `.jobsworth.yml`.

## Related

`expr-arity`
