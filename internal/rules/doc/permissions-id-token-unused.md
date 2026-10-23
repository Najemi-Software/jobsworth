# permissions-id-token-unused

## What it checks

`id-token: write` granted while no step uses an OIDC-consuming action (`aws-actions/configure-aws-credentials`,
`google-github-actions/auth`, `azure/login`, `hashicorp/vault-action`, `sigstore/*`, plus `security.oidc_actions`) and
no `run:` requests a token.

## Why it matters

The grant is a hardening regression for nothing.

## Example: flagged

```yaml
name: Example
on: push
permissions:
  id-token: write
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - run: echo hi
```

## Example: passes

```yaml
name: Example
on: push
permissions:
  id-token: write
  contents: read
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: aws-actions/configure-aws-credentials@0123456789abcdef0123456789abcdef01234567
```

## When it does not fire

Jobs with an OIDC consumer.

## Suppress

`# jobsworth-ignore permissions-id-token-unused` on the line, or
`rules: { permissions-id-token-unused: off }` in `.jobsworth.yml`.

## Related

`permissions-write-all`
