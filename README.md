# jobsworth

A static checker for GitHub Actions. It reads your workflow and action
files, follows every `uses:` to the reusable workflow or action it names,
and validates the whole graph: inputs against declarations, job outputs
against what steps actually write, secrets against what callers pass,
checkouts against the local actions that need them, expressions against
the shapes of the values they index, and more. 192 rules, each with a
doc, an example that triggers it, and one that does not.

It runs offline once its cache is warm, needs no YAML schema file, and
explains every finding in the context that produced it.

```
$ jobsworth check
FAIL .github/workflows/release.yml
  .github/workflows/release.yml:13:21  error  needs-unknown
    job 'deploy' needs unknown job 'nope'
  .github/workflows/release.yml:15:14  error  needs-output-undeclared
    job 'deploy' references needs.build.outputs.sha, but job 'build' declares no output 'sha'
  .github/workflows/release.yml:7:7  warning  output-unused
    job 'build' declares output 'version' but nothing references it
```

## Install

Prebuilt binaries for Linux, macOS and Windows:

```sh
curl -fsSL https://raw.githubusercontent.com/najemi-software/jobsworth/master/install.sh | sh
```

The script installs the latest release into `/usr/local/bin`. Pass a tag
to pin a version, or set `BINDIR` to install elsewhere:

```sh
BINDIR=~/.local/bin sh install.sh v0.4.0
```

From source, with Go 1.26 or later:

```sh
go install github.com/najemi-software/jobsworth@latest
```

## Quick start

```sh
cd your-repo
jobsworth init          # writes .jobsworth.yml, pre-filled from your workflows
jobsworth check         # validates .github/workflows/*.yml and everything they use
```

`check` exits 0 when clean, 1 when a finding reaches the `--fail-on`
severity (`error` by default), and 2 on a usage or configuration error.

For a reusable workflow or action in another repository, `check` fetches
it from GitHub through the API. Set a token so private repositories and
higher rate limits work. The first of these wins:

1. `JOBSWORTH_TOKEN`
2. `GITHUB_TOKEN` or `GH_TOKEN`
3. `gh auth token`, if the GitHub CLI is installed and logged in

Fetched files land in a cache under the user cache directory (or
`JOBSWORTH_CACHE_DIR`). Tags are re-resolved after a TTL, branches
sooner, SHAs never. `--offline` uses the cache and never touches the
network.

## What it checks

Rules are grouped in families. Every rule has an id, a default severity,
and a page in [`docs/rules.md`](docs/rules.md).

| Family | Examples |
|---|---|
| inputs, calls | an input referenced but never declared, a `with:` key the callee does not take, a required input missing, a literal that does not fit the declared type |
| jobs, steps | `needs:` on an unknown job, `needs.X.outputs.Y` that job X never declares, a step output read before the step that writes it, a step id nothing references |
| bash | `$GITHUB_OUTPUT` lines that never reach the file, `::set-output`, bad `GITHUB_ENV` keys, unterminated multi-line writes, env read in the step that set it |
| secrets | a secret the reusable workflow never declares, one the caller does not pass, `secrets: inherit` on a callee with nothing to inherit, a secret echoed or written to an artifact |
| checkout | a `./` action used before any checkout, or after a checkout of another repository, or outside a sparse-checkout list. Composite wrappers around `actions/checkout` are recognised from their content |
| shapes, expressions | `fromJSON(...)` chains indexed with a field the value does not have, unknown functions, wrong arity, type-mismatched comparisons, contexts used where GitHub does not provide them |
| triggers, structure | unknown events and filter keys, impossible cron dates, retired runner labels, duplicate workflow names, jobs no path can reach |
| security | untrusted context interpolated into `run:` or `github-script`, including through inputs and step outputs; `pull_request_target` with an untrusted checkout; unpinned actions; over-broad permissions |
| conditions, reachability | conditions that are always false, contradictory status functions, jobs gated on something no caller ever passes, outputs consumed from steps that cannot have run |
| performance | unused or redundant checkouts, jobs that could be one, whole-workspace artifacts |
| scripts | shellcheck and interpreter parse checks for `run:` steps, when the tools are installed |

Findings carry a category (`correctness`, `security`, `hygiene`,
`performance`); `check --category security` shows only those.

```sh
jobsworth rules                        # the registry with effective severities
jobsworth rules --family secrets       # one family
jobsworth rules needs-output-undeclared # one rule's doc
```

## Explaining a finding

`explain` re-runs the check on one file and shows the context behind a
finding: the reference, what was declared, where the value came from.

```
$ jobsworth explain .github/workflows/release.yml:15
.github/workflows/release.yml:15:14  error  needs-output-undeclared
  job 'deploy' references needs.build.outputs.sha, but job 'build' declares no output 'sha'

context:
  reference      needs.build.outputs.sha   at :15  (job deploy / step #1 / run)
  job 'deploy'   needs: [build]   direct
  job 'build'    outputs: [version]   at :7

suppress:
  add on line 15:    # jobsworth-ignore needs-output-undeclared
  or in .jobsworth.yml:   rules: { needs-output-undeclared: off }
```

`explain --last` walks the findings of the previous `check` run in the
current directory. `explain --checkouts <job> <file>` shows what the
tool believes a job's checkout state is, including the wiring of any
composite wrapper it inferred a checkout from.

## Shapes

`check` infers the shape of values that flow through outputs, `fromJSON`,
files read in scripts, `jq` and `yq` filters, and matrix definitions.
`shape` lets you ask about one expression and see the derivation:

```
$ jobsworth shape --at deploy .github/workflows/release.yml 'fromJSON(needs.set-vars.outputs.canary)[0].cluster'
fromJSON(needs.set-vars.outputs.canary)[0].cluster
  = String

why:
  needs.set-vars.outputs.canary
    lifted from jobs.set-vars.outputs.canary: ${{ steps.build-clusters-matrix.outputs.canary }} (line 9)
      written by step 'build-clusters-matrix' (line 17):  echo "canary=$CANARY_MATRIX" >>$GITHUB_OUTPUT
        $CANARY_MATRIX (line 16):  $(yq e .canary -o=json -M $DATA_FILE | jq -c)
          file .github/workflows/data/clusters.yml parsed as YAML: Object{canary: Array[Object{cluster: String, namespace: String}], …}
          yq '.canary': … → Array[Object{cluster: String, namespace: String}]
  fromJSON(String ⊂ JSON → X) = X
  [0] → Object{cluster: String, namespace: String}
  .cluster → String
```

`shape --all <file>` lists every expression in a file with its shape,
and the reason wherever the answer is `Any`.

## The dependency graph

`graph` shows what `check` would look at and where each piece comes
from, without validating anything:

```
$ jobsworth graph .github/workflows/build.yml
build.yml  (workflow, entry)
├─ job build / step #1    → actions/checkout@v4  (node-action, fetched, tag → 2db1d53, cache hit)
├─ job build / step greet → ./.github/actions/greet  (composite-action, local)
├─ job build / step #3    → ./.github/actions/nested  (composite-action, local)
│  └─ step #1 → ./.github/actions/greet  ↑
├─ job build / step #4    → docker://alpine:3.19  (docker-action, skipped)
└─ job call               → ./.github/workflows/reusable.yml  (workflow, local)

6 nodes: 1 entry, 3 local, 1 fetched (1 cache hit, 0 misses, 0 stale), 1 skipped
```

`--format dot` and `--format mermaid` render the same graph for
documentation; `--callers <file>` inverts it to show who reaches a
local workflow or action.

## Configuration

`init` inspects the repository and writes `.jobsworth.yml` at its root.
Every key is optional.

```yaml
schema: 1

# External repositories checked out next to this one: validated from
# disk instead of fetched. `init` detects these from sibling clones.
repos:
  acme/tools: ../acme-tools

# References to never fetch, parse, or validate.
skip:
  actions:
    - some-org/paid-action

# Per-rule severity: error, warning, note, or off.
rules:
  step-output-unused: off
  input-unused: warning

# Lowest severity that makes the exit code non-zero. Default: error.
fail_on: warning

# What `check` validates when given no arguments.
entries:
  - .github/workflows/*.yml

security:
  trusted_owners: [acme]        # owners whose unpinned actions are not flagged
  strict: true                  # turns on checkout-persist-credentials

scripts:
  shellcheck:
    enabled: auto               # auto | true | false
    min_severity: warning
```

Other sections tune the bash analyser (`bash`), shape inference
(`shapes`), fetching (`fetch`), the cache (`cache`), event and runner
tables (`triggers`, `structure`), the performance rules (`performance`)
and the enrichment lookups (`enrich`). `init --full` writes every key
with its default and a comment. `rules --config` shows the effective
severity of every rule under a given file.

Findings can also be suppressed in place:

```yaml
    needs: [build, lint]  # jobsworth-ignore needs-unknown -- lint is generated
```

`jobsworth-ignore-start <rule>` and `jobsworth-ignore-end` bracket a
region; `jobsworth-ignore-file <rule>` covers the file. A suppression
that hides nothing is reported as `suppress-unused`, so they do not
rot. `check --show-suppressed` lists what was hidden.

## Enrichment

`check --enrich` makes read-only GitHub API calls about the repository
being checked and uses the answers to suppress, upgrade, or add findings:
a missing `permissions:` block is dropped when the repository default is
read-only, a `runs-on:` label the organisation's runners expose is not
unknown, an environment the repository does not have is an error, a
secret no scope defines is a warning, an archived action is flagged.
The lookups are cached for an hour (`enrich.ttl`) and only ever narrow
or sharpen what `check` already reports. `--offline` and `--enrich`
cannot be combined.

## In CI

`init --workflow` writes a workflow that runs `check` on pull requests
touching `.github/`. `init --pre-commit` prints a pre-commit hook. Both
pin the running binary's version.

For a repository whose workflows call private reusable workflows or
actions, the default `GITHUB_TOKEN` cannot fetch them; pass a token with
read access to those repositories, or link them with `repos:` and check
them out alongside.

## The cache

```sh
jobsworth cache stats     # size, entry counts, last run's hit/miss numbers
jobsworth cache ls        # entries, most recently used first
jobsworth cache warm      # fetch everything the entries reach, without validating
jobsworth cache verify    # find corrupt or unexpected entries (--fix removes them)
jobsworth cache clear     # everything, or --refs, --content, --expired, --older-than 30d
```

## Development

```sh
go test ./...                       # the whole suite; no network, no git, no real tools
go test ./... -update               # regenerate goldens and docs/rules.md
go test -race -count=1 ./...        # what CI runs
```

Tests talk to an in-process fake GitHub served from `testdata/remote`,
with deterministic stand-ins for shellcheck and the interpreters on
`PATH`. Every rule has a fixture under `testdata/cases` that expects it
and one, named after it with an `_ok` suffix, that does not; the
registry test enforces both. `docs/rules.md` is generated from the rule
docs under `internal/rules/doc`; regenerate it with
`go test ./cmd/rules -update` after changing one.

Adding a rule means: register it in `internal/rules/registry.go`, write
its doc with a flagged and a passing example, add the two fixtures, and
run the suite. The doc examples are themselves tests.
