# output-behind-condition

<!-- jobsworth-test: enable -->

## What it checks

A job output whose value comes from a step with an `if:`, consumed by another job without checking `needs.J.result`.

## Why it matters

The output may be empty. Off by default; noisy on real corpora.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  a:
    runs-on: ubuntu-latest
    outputs:
      v: ${{ steps.s.outputs.v }}
    steps:
      - id: s
        if: github.ref == 'refs/heads/main'
        run: echo "v=1" >> "$GITHUB_OUTPUT"
  b:
    runs-on: ubuntu-latest
    needs: a
    steps:
      - run: echo ${{ needs.a.outputs.v }}
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  a:
    runs-on: ubuntu-latest
    outputs:
      v: ${{ steps.s.outputs.v }}
    steps:
      - id: s
        if: github.ref == 'refs/heads/main'
        run: echo "v=1" >> "$GITHUB_OUTPUT"
  b:
    runs-on: ubuntu-latest
    needs: a
    if: needs.a.result == 'success'
    steps:
      - run: echo ${{ needs.a.outputs.v }}
```

## When it does not fire

Consumers that check the result, and unconditional producing steps.

## Suppress

`# jobsworth-ignore output-behind-condition` on the line, or
`rules: { output-behind-condition: off }` in `.jobsworth.yml`.

## Related

`step-output-conditional`
