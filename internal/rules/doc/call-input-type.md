# call-input-type

## What it checks

A literal passed in `with:` is not representable in the callee's declared
type: text for `number`, anything but a bare `true`/`false` for `boolean`,
or a value outside `options:` for `choice`.

## Why it matters

GitHub rejects the caller at parse time. Quoted booleans (`"true"`) are
the classic case: YAML makes them strings.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    with:
      env: prod
      count: three
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  call:
    uses: ./.github/workflows/reusable.yml
    with:
      env: prod
      count: 3
      strict: ${{ github.event_name == 'push' }}
```

## When it does not fire

The value is an expression, the callee type is `string` or undeclared, or
the callee could not be loaded.

## Suppress

`# jobsworth-ignore call-input-type` on the `with` value's line, or
`rules: { call-input-type: off }` in `.jobsworth.yml`.

## Related

`input-default-type`.
