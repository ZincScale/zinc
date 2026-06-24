#!/usr/bin/env bash
# Cut a zc release: build the artifact, pin install.sh to this version, tag, create the
# GitHub release, upload assets. Idempotent-ish; safe to re-run if a step fails partway.
#
#   GITHUB_TOKEN=<PAT> ./release.sh 0.1.0
#
# Token: a PAT with `repo` scope (classic) or a fine-grained PAT with Contents: read+write
# on ZincScale/zinc. Egress to api.github.com + uploads.github.com required.
#
# Monorepo notes (see RELEASING.md):
#  - tag is `zc-vX.Y.Z`, NOT `vX.Y.Z` — the bare v* namespace is zinc-go's and its CI
#    releases on v* tags. zc-v* matches nothing, so no spurious Actions.
#  - the release is created with make_latest=false so it doesn't hijack the repo's
#    "latest release" (which zinc-go owns). install.sh fetches by explicit tag anyway.
set -euo pipefail
cd "$(dirname "$0")"

VER="${1:?usage: GITHUB_TOKEN=... ./release.sh <version, e.g. 0.1.0>}"
TAG="zc-v$VER"
REPO="${ZC_REPO:-ZincScale/zinc}"
: "${GITHUB_TOKEN:?set GITHUB_TOKEN (PAT with repo / Contents:write)}"

echo "==> building zc $VER"
bash package.sh "$VER"
mkdir -p dist/release
cp "dist/zc-$VER.tar.gz" dist/release/zc.tar.gz
cp install.sh dist/release/install.sh
( cd dist/release && sha256sum zc.tar.gz install.sh > SHA256SUMS )

# Pin the published install.sh (and its self-reference) to this tag, commit + push so the
# tagged tree matches what users download.
echo "==> pinning install.sh to $TAG"
sed -i "s|ZC_VERSION:-zc-v[0-9.]*|ZC_VERSION:-$TAG|" install.sh
sed -i "s|releases/download/zc-v[0-9.]*/install.sh|releases/download/$TAG/install.sh|" install.sh
cp install.sh dist/release/install.sh
( cd dist/release && sha256sum zc.tar.gz install.sh > SHA256SUMS )
if ! git diff --quiet install.sh; then
  git add install.sh
  git commit -q -m "release: pin install.sh to $TAG"
  git push origin HEAD
fi

echo "==> tagging $TAG"
git tag -a "$TAG" -m "zc $VER — BEAM toolchain" 2>/dev/null || echo "  tag exists, reusing"
git push origin "$TAG"

echo "==> creating GitHub release $TAG (make_latest=false)"
api() { curl -fsSL -H "Authorization: Bearer $GITHUB_TOKEN" \
  -H "Accept: application/vnd.github+json" "$@"; }
BODY="zc $VER — Zinc-on-BEAM toolchain.\n\nInstall:\n\`\`\`\ncurl -fsSL https://github.com/$REPO/releases/download/$TAG/install.sh | sh\n\`\`\`"
RID=$(api -X POST "https://api.github.com/repos/$REPO/releases" \
  -d "{\"tag_name\":\"$TAG\",\"name\":\"zc $VER\",\"make_latest\":\"false\",\"body\":\"$BODY\"}" \
  | grep -m1 -o '"id"[[:space:]]*:[[:space:]]*[0-9]*' | grep -o '[0-9]*')
[ -n "$RID" ] || { echo "release create failed (already exists? check GitHub)"; exit 1; }

echo "==> uploading assets"
for f in zc.tar.gz install.sh SHA256SUMS; do
  api -X POST -H "Content-Type: application/octet-stream" \
    --data-binary "@dist/release/$f" \
    "https://uploads.github.com/repos/$REPO/releases/$RID/assets?name=$f" >/dev/null
  echo "  uploaded $f"
done

echo
echo "released: https://github.com/$REPO/releases/tag/$TAG"
echo "install : curl -fsSL https://github.com/$REPO/releases/download/$TAG/install.sh | sh"
