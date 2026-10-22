# shellcheck-codes

## What it checks

Every bash and sh step is sent to shellcheck (`--format=json1`, `--shell=bash` or `sh`, `${{ }}` replaced by a
placeholder) when the tool is on PATH or named in `scripts.shellcheck.path`. Each code becomes its own rule id,
`shellcheck-SC2086`, so codes can be configured one by one; shellcheck errors are errors, warnings warnings, info and
style notes, and `scripts.shellcheck.min_severity` (default warning) drops the rest. `SC1090` and `SC1091` (sourcing)
are off by default.

## Why it matters

Shellcheck catches the quoting and expansion mistakes the built-in analyser does not model. When the tool is absent
every derived rule is silently off and `rules` lists the umbrella as unavailable; `scripts.shellcheck.enabled: true`
makes absence a finding.

## Example: flagged

```yaml
name: Example
on: push
permissions: read-all
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: ls $FILES
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
      - run: ls "$FILES"
```

## When it does not fire

Non-bash shells, an absent tool, and codes disabled by config or by shellcheck's own `# shellcheck disable=` comments.

## Suppress

`# jobsworth-ignore shellcheck-SC2086` on the line, or
`rules: { shellcheck-SC2086: off }` in `.jobsworth.yml`.

## Related

`bash-syntax`
