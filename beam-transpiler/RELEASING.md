# Releasing `zc`

> **Status: deferred.** Don't cut a public release until the toolchain is roughly
> halfway to a complete product. Everything below is ready to run when that time comes.

## TL;DR

```sh
GITHUB_TOKEN=<PAT> ./release.sh 0.1.0
```

That builds the artifact, pins `install.sh` to the tag, tags `zc-v0.1.0`, creates the
GitHub release, and uploads the assets. Afterwards anyone can:

```sh
curl -fsSL https://github.com/ZincScale/zinc/releases/download/zc-v0.1.0/install.sh | sh
```

## Prerequisites

- **A token.** A classic PAT with `repo` scope, or a fine-grained PAT scoped to
  `ZincScale/zinc` with **Contents: read & write**. Pass it as `GITHUB_TOKEN`.
- **Egress** to `api.github.com` and `uploads.github.com` (works here via Java/curl TLS).
- A clean working tree on `master` (the script commits the `install.sh` pin and pushes).

## Why the odd tag + flags (monorepo gotchas)

This repo is a monorepo and `zinc-go` lives beside us. Two consequences the release
machinery handles for you:

1. **Tag is `zc-vX.Y.Z`, never `vX.Y.Z`.** `zinc-go`'s CI (`.github/workflows/zinc-go.yml`)
   triggers a *release* on any `v*` tag, and `v0.7`/`v0.7.1` are already zinc-go's. A
   `zc-v*` tag matches nothing, so cutting a zc release fires **no Actions**.
2. **Release is created with `make_latest=false`.** Otherwise `zc-v0.1.0` (newer by date)
   would become the repo's "latest release" and mislead anyone pulling zinc-go's `latest`.
   `install.sh` fetches by **explicit tag**, so it never needs "latest" anyway.

## What `release.sh <version>` does

1. `package.sh` → `dist/zc-<ver>.tar.gz` (self-contained `zc.jar` + `rebar_zinc` + `bin/zc`).
2. Pins `install.sh`'s default `ZC_VERSION` + bootstrap URL to `zc-v<ver>`; commits + pushes.
3. Stages `dist/release/{zc.tar.gz, install.sh, SHA256SUMS}`.
4. `git tag -a zc-v<ver>` + push (no `v*` → no zinc-go CI).
5. `POST /releases` with `make_latest=false`; uploads the three assets.

## Verify after release

```sh
ZC_HOME=$(mktemp -d) sh -c 'curl -fsSL \
  https://github.com/ZincScale/zinc/releases/download/zc-v<ver>/install.sh | sh'
# then: zc version ; zc init demo && cd demo && zc run
```

## Manual fallback (no script)

Build + stage:

```sh
bash package.sh <ver>
mkdir -p dist/release && cp dist/zc-<ver>.tar.gz dist/release/zc.tar.gz
cp install.sh dist/release/ && ( cd dist/release && sha256sum zc.tar.gz install.sh > SHA256SUMS )
```

Then create a GitHub release on tag `zc-v<ver>` (web UI or `gh release create`),
**mark it _not_ latest**, and attach the three files from `dist/release/`.

## Pre-release checklist

- [ ] `./e2e.sh` green (legal-Java gate + cases)
- [ ] `zc/test.sh` green
- [ ] version bumped where it's reported (`zc version`, `zinc.toml` templates)
- [ ] CHANGELOG / release notes drafted
- [ ] decide: is this the release where we add CI? (until then, releases are manual on purpose)
