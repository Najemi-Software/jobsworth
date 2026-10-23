# shape-mismatch

## What it checks

A member access chain that cannot succeed on the value's inferred shape:
a field missing from a closed object, a field on an array or primitive,
an integer index on an object, and so on (medium §6.1). The message shows
the chain, the failing step, and where the shape came from.

## Why it matters

The expression evaluates to empty or fails at runtime; usually a typo in
a field name or a wrong assumption about a payload or a JSON output.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ fromJSON('{"version":"1.2.3"}').verison }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ fromJSON('{"version":"1.2.3"}').version }}
```

## When it does not fire

Anything unknown is `any` and never reported: open objects (GitHub
payloads by default), dynamic bracket indexes, outputs of unparsed
scripts. False positives are worse than silence here.

After a `.*` wildcard the rest of the chain maps over the elements:
`needs.*.result`, `labels.*.name`, `items.*.id` are arrays of the
element's field, and only a field the elements lack is reported. A
matrix whose `include:` (or an axis) is an expression is an open object,
since its fields come from the resolved value.

## Suppress

`# jobsworth-ignore shape-mismatch` on the expression's line, or
`rules: { shape-mismatch: off }` in `.jobsworth.yml`.

## Related

`fromjson-non-string`.
