# environment-unknown

<!-- jobsworth-test: enrich self=example/app -->

## What it checks

`environment:` names a deployment environment the repository does not have. Enrich-only (`check --enrich`).

## Why it matters

The job waits for an environment that never approves, or GitHub creates an unprotected one on the fly.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    environment: prodution
    steps:
      - run: echo deploy
```

## Example: passes

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - run: echo deploy
```

## When it does not fire

Without `--enrich`, expression-valued environments, and environments the API lists.

## Suppress

`# jobsworth-ignore environment-unknown` on the line, or
`rules: { environment-unknown: off }` in `.jobsworth.yml`.

## Related

`environment-no-protection`
