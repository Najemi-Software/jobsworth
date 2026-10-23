# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.1] - 2026-10-23

### Added

- `LICENSE` file: the project is released under the MIT License.

## [0.1.0] - 2026-10-23

Initial release of `jobsworth`, a static checker for GitHub Actions workflows
and actions. It parses workflow and action files, follows every `uses:`
reference (local, linked, or fetched from GitHub into a local cache), and
validates the contract between callers and callees.

### Added

- `check` command: walks one or more entry workflows and their callees and
  reports findings as text or JSON, with `--fail-on`, `--category`,
  `--input k=v` to seed `workflow_dispatch` inputs, `--offline`, `--no-fetch`,
  `--enrich` for read-only GitHub API lookups, and inline
  `# jobsworth-ignore` suppressions with unused-suppression reporting.
- 192 rules in 19 families, each with a doc, a triggering fixture, and a
  non-triggering fixture: inputs, calls, jobs, steps, bash, checkout,
  secrets, values, shapes, fetch, structure, suppress, expressions,
  triggers, security, conditions, reachability, performance, and scripts.
  Rules carry a category (correctness, security, hygiene, performance),
  a severity, aliases, and related ids; `docs/rules.md` is generated from
  the registry.
- Bash analysis of `run:` steps: `$GITHUB_OUTPUT`, `$GITHUB_ENV`, and
  `$GITHUB_PATH` writes across paths, heredoc forms, loop variables,
  brace expansion, positional arguments, cross-step variable seeding, and
  reads of outputs that were never written.
- Shape inference: the structure of JSON and YAML values is tracked
  through `fromJSON`, `toJSON`, step and job outputs, caller `with:`
  values, `jq` and `yq` filters, file reads, and matrix strategies, so
  member accesses that cannot resolve are reported as `shape-mismatch`.
- Checkout inference: composite actions that call `actions/checkout`
  are recognised from their content, with effective `path`,
  `repository`, `ref`, sparse list, and credential settings computed from
  the wrapper's wiring and the caller's `with:`; no configuration list is
  needed.
- Security rules: expression injection into `run:` and `github-script`
  sinks, including taint carried through `with:` forwarding across
  callees and through bash variables into step and job outputs;
  privileged-trigger checkouts; unpinned actions; permissions scopes and
  levels; credential hygiene.
- Condition analysis: a three-valued evaluator over `if:` expressions
  with caller value sets, `workflow_dispatch` domains, and status
  function contradictions.
- Optional script tools: `shellcheck` findings as derived
  `shellcheck-SC<code>` ids, and parse-only checks for Python,
  PowerShell, Node, and Ruby steps when the interpreter is installed.
- `explain` command: a rule's documentation, or a finding at
  `file:line` with the context that produced it (checkouts and their
  wiring, caller values, bash provenance, shape chains); `--last` for
  the previous run and `--checkouts <job>` for a job's checkout state.
- `shape` command: evaluate an expression at a point in a workflow and
  print its inferred shape, the provenance behind it, and the member
  accesses it accepts; `--all` lists every expression in a file.
- `graph` command: the callee graph of an entry as a tree or JSON.
- `init` command: inspects a repository and writes a `.jobsworth.yml`
  with detected sibling checkouts, skip lists, and a CI workflow on
  request.
- `rules` command: lists rules by family, category, or effective
  severity under a config, in text, JSON, or markdown.
- `cache` command: `ls`, `stats`, `verify`, `warm`, and `clear` for the
  content-addressed fetch cache, with TTLs for tags and branches and an
  atomic, lock-protected on-disk layout.
- `.jobsworth.yml` configuration: `repos` links to local checkouts,
  `skip` lists, `rules` overrides, `fail_on`, `entries`, `bash`,
  `shapes`, `fetch`, `cache`, `security`, `triggers`, `structure`,
  `scripts`, `performance`, and `enrich` sections, with unknown keys
  rejected and rule ids suggested on typos.
- Token resolution from `JOBSWORTH_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN`,
  or the `gh` CLI; rate-limit, retry, and offline behaviour against the
  GitHub API.
- Test harness: an in-process fake GitHub server, a fixture corpus with
  `expect:` headers, golden outputs for every command, and a registry
  coverage test; CI runs `go vet` and the race-enabled suite on every
  push.

[0.1.1]: https://github.com/Najemi-Software/jobsworth/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/Najemi-Software/jobsworth/releases/tag/v0.1.0
