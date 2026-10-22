# expr-format-placeholders

## What it checks

`format()` with a literal format string whose `{n}` placeholders do not match its arguments: a placeholder index with no
argument is an error; an argument no placeholder uses is a warning. `{{` and `}}` are escapes and not placeholders.

## Why it matters

A placeholder without an argument makes GitHub reject the expression; an unused argument is a sign the string and the
arguments drifted apart.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ format('{0} and {2}', github.sha, github.ref) }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo ${{ format('{0} and {1} {{literal}}', github.sha, github.ref) }}
```

## When it does not fire

Format strings that are not literals.

## Suppress

`# jobsworth-ignore expr-format-placeholders` on the line, or
`rules: { expr-format-placeholders: off }` in `.jobsworth.yml`.

## Related

`expr-arity`
