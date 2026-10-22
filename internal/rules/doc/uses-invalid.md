# uses-invalid

## What it checks

A `uses:` is malformed (not `owner/repo[/path]@ref`, escapes the repo) or
names the wrong kind of callee: a job-level `uses:` pointing at an
action, or a step-level `uses:` pointing at a workflow.

## Why it matters

GitHub rejects the workflow at parse time.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  greet:
    uses: ./.github/actions/greet
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
```

## When it does not fire

A missing `@ref` is `uses-no-ref`; a target that does not exist is
`uses-not-found` or `fetch-not-found`.

## Suppress

`# jobsworth-ignore uses-invalid` on the `uses:` line, or
`rules: { uses-invalid: off }` in `.jobsworth.yml`.

## Related

`uses-no-ref`.
