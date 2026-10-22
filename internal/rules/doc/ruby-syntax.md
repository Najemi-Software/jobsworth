# ruby-syntax

<!-- jobsworth-test: enable -->

## What it checks

A `shell: ruby {0}` step that `ruby -c` rejects. Off by default.

## Why it matters

The step fails at runtime.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - shell: ruby {0}
        run: |
          end end
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
      - shell: ruby {0}
        run: |
          puts 'ok'
```

## When it does not fire

Bodies that parse, and an absent interpreter.

## Suppress

`# jobsworth-ignore ruby-syntax` on the line, or
`rules: { ruby-syntax: off }` in `.jobsworth.yml`.

## Related

`node-syntax`
