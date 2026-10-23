## inputs

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `input-undeclared` | error | error | 0.1 | Input is referenced but not declared |
| `input-unused` | warning | error \* | 0.1 | Input is declared but never referenced |
| `input-default-type` | error | error | 0.1 | Input default is not representable in its declared type |
| `choice-default-not-option` | error | error | 0.2 | Choice input's default is not one of its options |
| `choice-no-options` | error | error | 0.2 | Choice input declares no options |

## calls

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `call-input-unknown` | error | error | 0.1 | Reusable workflow call supplies an input the callee does not declare |
| `call-input-missing` | error | error | 0.1 | Reusable workflow call omits a required input |
| `call-input-type` | error | error | 0.1 | Reusable workflow call passes a literal of the wrong type |
| `step-input-unknown` | error | error | 0.2 | Step supplies an input the action does not declare |
| `step-input-missing` | error | error | 0.2 | Step omits a required action input |
| `step-input-type` | error | error | 0.2 | Step passes a literal of the wrong type to a typed input |
| `step-input-choice` | error | error | 0.2 | Step passes a literal outside a choice input's options |
| `step-with-on-run` | error | error | 0.2 | Run step has a with block, which the runner ignores |
| `step-run-and-uses` | error | error | 0.2 | Step has both run and uses |

## jobs

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `needs-unknown` | error | warning \* | 0.1 | Job needs a job that does not exist |
| `needs-self` | error | error | 0.1 | Job lists itself in needs |
| `needs-cycle` | error | error | 0.1 | Needs edges form a cycle |
| `needs-output-job-unknown` | error | error | 0.1 | Expression references outputs of a job that does not exist |
| `needs-output-not-needed` | error | error | 0.1 | Job references outputs of a job it does not need |
| `needs-output-undeclared` | error | error | 0.1 | Job references an output the needed job does not declare |
| `call-output-job-unknown` | error | error | 0.1 | Workflow output references a job that does not exist |
| `call-output-undeclared` | error | error | 0.1 | Workflow output references a job output that is not declared |
| `output-unused` | warning | warning | 0.1 | Job output is declared but nothing references it |

## steps

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `step-id-unknown` | error | error | 0.2 | Step output reference names an id no step has |
| `step-id-duplicate` | error | error | 0.2 | Two steps in one scope share an id |
| `step-id-invalid` | error | error | 0.2 | Step id has characters outside [A-Za-z0-9_-] or starts with a digit |
| `step-forward-ref` | error | error | 0.2 | Step references a step declared later in the scope |
| `step-output-unwritten` | error | error | 0.2 | Step output is referenced but never written |
| `step-output-conditional` | off | off | 0.2 | Step output is only written on some paths |
| `step-output-action-undeclared` | error | error | 0.2 | Step output reference names an output the action does not declare |
| `step-output-not-run` | error | error | 0.2 | Step output reference points at a step with neither run nor uses |
| `step-output-unused` | note | note | 0.2 | Step writes an output nothing references |
| `step-id-unreferenced` | off | off | 0.2 | Step has an id nothing references |
| `action-output-unlifted` | error | error | 0.2 | Composite action output references a step output that does not exist |
| `action-output-unused` | note | note | 0.2 | Composite action declares an output no local caller consumes |
| `composite-shell-missing` | error | error | 0.2 | Composite run step has no shell |

## bash

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `bash-syntax` | error | error | 0.2 | Run script does not parse as bash |
| `bash-deprecated-command` | error | error | 0.2 | Script emits a workflow command the runner dropped in 2023 |
| `bash-output-unredirected` | warning | warning | 0.2 | Echo of a referenced output key goes to the log, not $GITHUB_OUTPUT |
| `bash-output-read` | error | error | 0.2 | Script reads $GITHUB_OUTPUT, which never holds other steps' state |
| `bash-path-kv-line` | warning | warning | 0.2 | KEY=VALUE line written to $GITHUB_PATH |
| `bash-env-bare-line` | warning | warning | 0.2 | Line without = written to $GITHUB_ENV |
| `bash-env-bad-key` | error | error | 0.2 | $GITHUB_ENV line has an empty or invalid key |
| `bash-env-reserved-key` | warning | warning | 0.2 | $GITHUB_ENV sets a key the runner owns |
| `bash-env-same-step` | warning | warning | 0.2 | Script reads a variable it just wrote to $GITHUB_ENV |
| `heredoc-unterminated` | error | error | 0.2 | KEY<<DELIM opener has no matching DELIM line |
| `heredoc-empty-delim` | error | error | 0.2 | KEY<< with no delimiter |
| `heredoc-escaped-newline` | warning | warning | 0.2 | Multi-line value written with literal \n in an echo without -e |
| `heredoc-delim-collision` | warning | warning | 0.2 | Common delimiter around a value from an opaque command |
| `heredoc-indented-form` | off | off | 0.2 | KEY<<-DELIM form, which the runner does not treat specially |

## checkout

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `local-action-no-checkout` | error | error | 0.2 | Local action used before any checkout in the job |
| `local-action-wrong-repo` | warning | warning | 0.2 | Local action will be loaded from a different repository's checkout |
| `local-action-sparse-miss` | error | error | 0.2 | Sparse checkout does not cover the local action's path |
| `local-action-after-subdir-only` | error | error | 0.2 | Only subdirectory checkouts precede a local action |
| `checkout-ref-unresolvable` | off | off | 0.2 | Checkout ref is an expression and cannot be verified |

## secrets

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `call-secret-unknown` | error | error | 0.2 | Reusable workflow call supplies a secret the callee does not declare |
| `call-secret-missing` | error | error | 0.2 | Reusable workflow call omits a required secret |
| `call-secret-literal` | error | error | 0.2 | Secret passed as a bare literal in source |
| `call-secret-inherit-partial` | off | off | 0.2 | secrets: inherit on a callee that declares no secrets |
| `secret-undeclared` | error | error | 0.2 | Reusable workflow references a secret it does not declare |
| `secret-unused` | note | note | 0.2 | Declared secret is never referenced |
| `secret-in-action` | error | error | 0.2 | Composite action references the secrets context, which does not exist there |
| `secret-github-token-declared` | note | note | 0.2 | GITHUB_TOKEN declared as a secret input |
| `secret-to-output` | warning | warning | 0.2 | Output value is a secret, which consumers receive empty |
| `secret-in-name` | warning | warning | 0.2 | Secret interpolated into a name, which is logged in clear |
| `secret-unknown-in-scope (enrich)` | warning | warning | 0.4 | secrets.X referenced but no repository or organisation secret has that name |

## values

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `value-set-mismatch` | error | error | 0.2 | Comparison against a literal outside the path's documented value set |
| `if-always-true-string` | error | error | 0.2 | if: is a plain string, which is always truthy |
| `if-double-wrapped` | error | error | 0.2 | if: joins two expression blocks into a string |
| `if-status-with-other` | warning | warning | 0.2 | if: combines always() with other conditions |

## shapes

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `shape-mismatch` | error | error | 0.2 | Member access that cannot succeed on the value's inferred shape |
| `fromjson-invalid-literal` | error | error | 0.2 | fromJSON of a string literal that is not valid JSON |
| `fromjson-non-string` | error | error | 0.2 | fromJSON applied to a value that is already an object or array |
| `jq-syntax` | error | error | 0.2 | jq filter in a run script does not parse |
| `shape-file-missing` | off | off | 0.2 | Script reads a literal JSON or YAML path that does not exist in the repo |
| `matrix-empty` | error | error | 0.4 | Matrix axis with an empty list |
| `matrix-exclude-no-match` | warning | warning | 0.4 | Matrix exclude entry that matches no combination |
| `matrix-strategy-type` | error | error | 0.4 | strategy.max-parallel or fail-fast of the wrong type |

## fetch

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `fetch-failed` | warning | warning | 0.1 | Callee could not be fetched from GitHub |
| `fetch-not-found` | error | error | 0.1 | Referenced repo, ref, or file does not exist on GitHub |
| `fetch-offline` | warning | warning | 0.1 | Callee is not cached and the run is offline |
| `uses-no-ref` | error | error | 0.1 | External uses reference has no @ref |
| `uses-invalid` | error | error | 0.1 | Uses reference is malformed or names the wrong kind of callee |
| `uses-dynamic` | note | off \* | 0.1 | Uses reference contains an expression and cannot be followed |
| `uses-not-found` | error | error | 0.1 | Local or linked uses reference does not exist on disk |
| `callee-unparseable` | error | error | 0.1 | Callee is not valid YAML or is neither a workflow nor an action |

## structure

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `file-invalid` | error | error | 0.1 | Entry file is unreadable, not valid YAML, or neither a workflow nor an action |
| `runs-on-unknown` | warning | warning | 0.4 | runs-on label that no GitHub-hosted or configured runner has |
| `runs-on-retired` | warning | warning | 0.4 | runs-on label GitHub has retired |
| `runs-on-matrix` | warning | warning | 0.4 | runs-on: ${{ matrix.os }} with an unknown or retired label in the axis |
| `runs-on-missing` | error | error | 0.4 | Non-uses job without runs-on: |
| `runs-on-group-and-labels` | off | off | 0.4 | runs-on group with no labels: |
| `job-empty-steps` | warning | warning | 0.4 | steps: [] runs a job that does nothing |
| `job-no-steps` | error | error | 0.4 | Job with neither steps: nor uses: |
| `step-null` | error | error | 0.4 | null entry in a steps list |
| `step-no-action` | error | error | 0.4 | Step with neither run: nor uses: |
| `job-orphan-outputs` | note | note | 0.4 | Job outputs that nothing needs or lifts |
| `job-unreachable` | warning | warning | 0.4 | Job whose needs chain passes through an if: false job |
| `job-timeout-missing` | off | off | 0.4 | Job without timeout-minutes: |
| `step-continue-on-error-outputs` | note | note | 0.4 | continue-on-error step whose outputs are consumed without an outcome check |
| `workflow-name-missing` | note | note | 0.4 | Workflow without name: |
| `workflow-name-duplicate` | note | note | 0.4 | Two workflows in the repo share a name: |
| `action-name-missing` | note | note | 0.4 | Action without name: |
| `action-description-missing` | note | note | 0.4 | Action without description: |
| `input-description-missing` | off | off | 0.4 | Input without description: |
| `name-expression` | warning | warning | 0.4 | name: with an expression, which is not evaluated there |
| `environment-unknown (enrich)` | error | error | 0.4 | environment: names an environment the repository does not have |
| `runs-on-group-unknown (enrich)` | error | error | 0.4 | runs-on group the organisation does not have |

## suppress

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `suppress-wildcard` | note | note | 0.1 | Suppression uses * and hides future rules too |
| `suppress-unclosed` | warning | warning | 0.1 | jobsworth-ignore-start has no matching jobsworth-ignore-end |
| `suppress-unknown-rule` | error | error | 0.1 | Suppression names a rule id that does not exist |
| `suppress-unused` | note | note | 0.1 | Suppression suppressed nothing |

## expressions

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `expr-unknown-function` | error | error | 0.4 | Call to a function GitHub Actions expressions do not have |
| `expr-arity` | error | error | 0.4 | Expression function called with the wrong number of arguments |
| `expr-format-placeholders` | error | error | 0.4 | format() placeholders and arguments do not line up |
| `expr-contains-types` | warning | warning | 0.4 | contains() applied to an object |
| `expr-hashfiles-absolute` | warning | warning | 0.4 | hashFiles() with a path outside the workspace |
| `expr-string-escape` | error | error | 0.4 | String literal is double-quoted or has an unescaped quote |
| `expr-comparison-type` | warning | warning | 0.4 | Comparison between a number or boolean and a string that can never match |
| `expr-env-in-if-job` | error | error | 0.4 | env.* used in a job-level if:, where the env context is empty |
| `expr-secrets-in-if` | error | error | 0.4 | secrets.* used in an if:, where the secrets context is unavailable |
| `expr-context-unavailable` | error | error | 0.4 | A context used where GitHub does not provide it |

## triggers

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `on-missing` | error | error | 0.4 | Workflow has no on: block |
| `on-unknown-event` | error | error | 0.4 | on: names an event GitHub does not have |
| `on-unknown-key` | error | error | 0.4 | Event child key the event does not accept |
| `on-unknown-type` | warning | warning | 0.4 | types: entry that is not an activity type of the event |
| `on-filter-conflict` | error | error | 0.4 | Both a filter and its -ignore form on one event |
| `on-filter-shape` | error | error | 0.4 | Filter value is not a string or list of strings |
| `on-filter-expression` | error | error | 0.4 | Expression inside an on: filter |
| `on-filter-glob` | warning | warning | 0.4 | Filter pattern with an unbalanced [ or trailing backslash |
| `on-filter-empty` | warning | warning | 0.4 | Empty branches:/paths: list matches nothing |
| `on-workflow-run-target` | error | error | 0.4 | workflow_run without workflows: |
| `on-workflow-run-unknown` | warning | warning | 0.4 | workflow_run names a workflow no local file declares |
| `on-push-tags-only` | off | off | 0.4 | push with tags: and no branches: no longer triggers on branch pushes |
| `cron-syntax` | error | error | 0.4 | schedule cron expression does not parse |
| `cron-too-frequent` | warning | warning | 0.4 | Schedule fires more often than every 5 minutes |
| `cron-impossible-date` | warning | warning | 0.4 | Schedule names a day no listed month has |
| `cron-quoted` | note | note | 0.4 | Cron string not quoted in YAML |
| `dispatch-input-type` | error | error | 0.4 | workflow_dispatch input type is not one GitHub supports |
| `call-input-type-decl` | error | error | 0.4 | workflow_call input type is not string, boolean or number |
| `dispatch-input-limit` | error | error | 0.4 | More than 25 workflow_dispatch inputs |
| `input-no-trigger` | error | error | 0.4 | inputs.* referenced in a workflow with no input-bearing trigger |
| `dispatch-input-event-spelling` | off | off | 0.4 | github.event.inputs.X where inputs.X would do |
| `concurrency-group-static` | note | note | 0.4 | Concurrency group without an expression serialises every run |
| `concurrency-cancel-on-push-main` | off | off | 0.4 | cancel-in-progress on a group keyed on github.ref in a deploying workflow |
| `on-filter-no-branch (enrich)` | note | note | 0.4 | A literal branches: entry that does not exist |

## security

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `injection-run` | warning | warning | 0.4 | Attacker-controlled context interpolated into a run: script |
| `injection-script` | warning | warning | 0.4 | Attacker-controlled context interpolated into github-script code |
| `injection-via-input` | warning | warning | 0.4 | Untrusted context passed to an input the callee interpolates into a sink |
| `prt-untrusted-checkout` | warning | warning | 0.4 | Privileged trigger checks out the pull request head |
| `prt-untrusted-checkout-and-run` | error | error | 0.4 | Privileged trigger checks out the pull request head and runs code from it |
| `prt-no-permissions` | warning | warning | 0.4 | Privileged-trigger workflow without a permissions: block |
| `uses-unpinned` | note | note | 0.4 | External action from an untrusted owner not pinned to a commit SHA |
| `uses-unpinned-no-comment` | off | off | 0.4 | SHA-pinned action without a version comment |
| `uses-docker-unpinned` | note | note | 0.4 | docker:// image without a digest |
| `permissions-write-all` | warning | warning | 0.4 | permissions: write-all |
| `permissions-missing` | warning | warning | 0.4 | No permissions: block; the token gets the repository default |
| `permissions-unknown-scope` | error | error | 0.4 | permissions: key that is not a documented scope |
| `permissions-unknown-level` | error | error | 0.4 | permissions: value other than read, write or none |
| `permissions-id-token-unused` | note | note | 0.4 | id-token: write granted and nothing consumes it |
| `checkout-persist-credentials` | off | off | 0.4 | Checkout keeps the token and a later step exposes the tree |
| `secret-to-artifact` | warning | warning | 0.4 | A secret is written to a file that an artifact upload covers |
| `secret-echo` | note | note | 0.4 | A secret is echoed |
| `self-hosted-pr-trigger` | warning | warning | 0.4 | Self-hosted runner on an unrestricted pull_request trigger |
| `cache-poisoning-path` | warning | warning | 0.4 | Cache key built from an untrusted path on a privileged trigger |
| `environment-no-protection (enrich)` | note | note | 0.4 | Deployment environment without protection on a privileged trigger |
| `uses-archived (enrich)` | warning | warning | 0.4 | The action's repository is archived |
| `uses-deprecated-tag (enrich)` | note | note | 0.4 | The resolved tag is more than one major behind the latest release |

## conditions

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `if-not-boolean` | warning | warning | 0.4 | if: whose value is a string or number, not a boolean |
| `if-string-compare-bool` | warning | warning | 0.4 | A string-valued step output compared with a boolean literal |
| `if-always-false` | warning | warning | 0.4 | if: that is false for every value |
| `if-literal-false` | note | note | 0.4 | if: false, the deliberate disablement |
| `if-status-contradiction` | warning | warning | 0.4 | Status functions that cannot hold together |
| `if-redundant-success` | off | off | 0.4 | if: success() alone, which is the default |
| `if-dead-for-all-callers` | warning | warning | 0.4 | if: false for every value any known caller passes |
| `if-dead-for-caller` | off | off | 0.4 | if: false for one caller's values |
| `expr-or-default-boolean` | warning | warning | 0.4 | boolean input || default drops false |
| `expr-and-or-ternary-falsy` | warning | warning | 0.4 | cond && x || y where x can be falsy |

## reachability

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `step-unreachable` | note | note | 0.4 | Step whose if: is always false while its outputs are consumed |
| `job-needs-skipped` | note | note | 0.4 | Job silently skipped when a conditional dependency skips |
| `output-behind-condition` | off | off | 0.4 | Job output from a conditional step consumed without a result check |

## performance

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `checkout-unused` | note | note | 0.4 | Checkout followed only by steps that do not touch the filesystem |
| `checkout-trailing` | note | note | 0.4 | Checkout as the last step |
| `checkout-redundant` | note | note | 0.4 | Second root checkout of the same repository and ref |
| `jobs-mergeable` | note | note | 0.4 | Two jobs in a chain that could be one |
| `setup-no-cache` | off | off | 0.4 | setup-* without cache: in a job that installs packages |
| `artifact-whole-workspace` | note | note | 0.4 | upload-artifact of the whole workspace |

## scripts

| ID | Default | Effective | Since | Summary |
|----|---------|-----------|-------|---------|
| `shellcheck-codes (unavailable)` | warning | warning | 0.4 | shellcheck findings, one derived id per code (shellcheck-SC2086) |
| `python-syntax (unavailable)` | warning | warning | 0.4 | A shell: python step that python cannot parse |
| `pwsh-syntax (unavailable)` | warning | warning | 0.4 | A shell: pwsh step that PowerShell cannot parse |
| `node-syntax (unavailable)` | off | off | 0.4 | A shell: node step that node cannot parse |
| `ruby-syntax (unavailable)` | off | off | 0.4 | A shell: ruby step that ruby cannot parse |

