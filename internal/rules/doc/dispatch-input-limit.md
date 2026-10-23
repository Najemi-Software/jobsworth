# dispatch-input-limit

## What it checks

More than 25 `workflow_dispatch` inputs.

## Why it matters

GitHub's hard limit; the workflow is rejected.

## Example: flagged

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      i0:
        type: string
      i1:
        type: string
      i2:
        type: string
      i3:
        type: string
      i4:
        type: string
      i5:
        type: string
      i6:
        type: string
      i7:
        type: string
      i8:
        type: string
      i9:
        type: string
      i10:
        type: string
      i11:
        type: string
      i12:
        type: string
      i13:
        type: string
      i14:
        type: string
      i15:
        type: string
      i16:
        type: string
      i17:
        type: string
      i18:
        type: string
      i19:
        type: string
      i20:
        type: string
      i21:
        type: string
      i22:
        type: string
      i23:
        type: string
      i24:
        type: string
      i25:
        type: string
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on:
  workflow_dispatch:
    inputs:
      a:
        type: string
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## When it does not fire

25 inputs or fewer.

## Suppress

`# jobsworth-ignore dispatch-input-limit` on the line, or
`rules: { dispatch-input-limit: off }` in `.jobsworth.yml`.

## Related

None.
