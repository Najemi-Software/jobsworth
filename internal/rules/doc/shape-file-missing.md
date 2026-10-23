# shape-file-missing

<!-- jobsworth-test: enable -->

## What it checks

A `cat`, `<`, `jq` or `yq` of a literal `.json`/`.yml` path that does not
exist in the repository at validation time.

## Why it matters

Usually fine: the file is generated earlier in the job. Off by default;
turn it on to find stale paths. When the file exists, its content gives
the shape of what the script writes from it.

## Example: flagged

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: cfg
        run: echo "cfg=$(cat missing.json)" >> "$GITHUB_OUTPUT"
      - run: echo ${{ steps.cfg.outputs.cfg }}
```

## Example: passes

```yaml
name: Example
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - id: cfg
        run: echo "cfg=$(cat config.json)" >> "$GITHUB_OUTPUT"
      - run: echo ${{ fromJSON(steps.cfg.outputs.cfg).retries }}
```

## When it does not fire

Dynamic paths, non-data files, and fetched files (their repos are not on
disk).

## Suppress

Leave the rule off, or `# jobsworth-ignore shape-file-missing` on the
`run:` line.

## Related

`shape-mismatch`.
