#!/usr/bin/env sh
# zc installer — the rustup model: one command lands the zc CLI + a managed runtime,
# no system Java, no system Erlang, no Docker.
#
#   curl -fsSL https://github.com/ZincScale/zinc/releases/latest/download/install.sh | sh
#
# Honors:
#   ZC_HOME           install root (default ~/.zc)
#   ZC_VERSION        release tag to pull (default: latest)
#   ZC_DIST_TARBALL   use a local zc tarball instead of downloading (offline/air-gapped)
#   ZC_SKIP_JRE=1     don't fetch the managed JRE (use host java)
#   ZC_SKIP_OTP=1     don't fetch the OTP runtime now (do it later: zc toolchain install)
set -eu

REPO="${ZC_REPO:-ZincScale/zinc}"
ZC_HOME="${ZC_HOME:-$HOME/.zc}"
JAVA_MAJOR=25

say() { printf 'zc-install: %s\n' "$1"; }
die() { printf 'zc-install: %s\n' "$1" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"; }

need uname; need tar
DL=""
command -v curl >/dev/null 2>&1 && DL="curl -fsSL -o"
[ -z "$DL" ] && command -v wget >/dev/null 2>&1 && DL="wget -qO"
[ -z "$DL" ] && [ -z "${ZC_DIST_TARBALL:-}" ] && die "need curl or wget"

# ---- platform ----
case "$(uname -s)" in
  Linux)  OS=linux ;;
  Darwin) OS=mac ;;
  *) die "unsupported OS: $(uname -s) (linux/mac only today)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64)  ARCH=x64 ;;
  aarch64|arm64) ARCH=aarch64 ;;
  *) die "unsupported arch: $(uname -m)" ;;
esac

mkdir -p "$ZC_HOME"
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT

# ---- 1. zc itself (jar + shim + rebar plugin) ----
TARBALL="$TMP/zc.tar.gz"
if [ -n "${ZC_DIST_TARBALL:-}" ]; then
  say "using local tarball $ZC_DIST_TARBALL"
  cp "$ZC_DIST_TARBALL" "$TARBALL"
else
  if [ "${ZC_VERSION:-latest}" = latest ]; then
    URL="https://github.com/$REPO/releases/latest/download/zc.tar.gz"
  else
    URL="https://github.com/$REPO/releases/download/$ZC_VERSION/zc.tar.gz"
  fi
  say "downloading $URL"
  $DL "$TARBALL" "$URL" || die "download failed: $URL"
fi
# tarball is zc-<ver>/{lib,bin}; strip the top dir so lib/ and bin/ land in $ZC_HOME
tar xzf "$TARBALL" -C "$ZC_HOME" --strip-components=1
[ -f "$ZC_HOME/lib/zc.jar" ] || die "tarball missing lib/zc.jar"
chmod +x "$ZC_HOME/bin/zc"
say "installed zc -> $ZC_HOME"

# ---- 2. managed JRE (so the box needs no system Java) ----
if [ -z "${ZC_SKIP_JRE:-}" ] && [ ! -x "$ZC_HOME/jre/bin/java" ]; then
  JURL="https://api.adoptium.net/v3/binary/latest/$JAVA_MAJOR/ga/$OS/$ARCH/jre/hotspot/normal/eclipse?project=jdk"
  say "downloading JRE $JAVA_MAJOR ($OS/$ARCH)"
  $DL "$TMP/jre.tar.gz" "$JURL" || die "JRE download failed"
  mkdir -p "$TMP/jre"; tar xzf "$TMP/jre.tar.gz" -C "$TMP/jre" --strip-components=1
  rm -rf "$ZC_HOME/jre"; mv "$TMP/jre" "$ZC_HOME/jre"
  say "installed JRE -> $ZC_HOME/jre"
fi

# ---- 3. PATH ----
case ":$PATH:" in
  *":$ZC_HOME/bin:"*) ;;
  *)
    for rc in "$HOME/.bashrc" "$HOME/.zshrc" "$HOME/.profile"; do
      [ -f "$rc" ] || continue
      grep -qs "$ZC_HOME/bin" "$rc" 2>/dev/null && continue
      printf '\n# zc\nexport PATH="%s/bin:$PATH"\n' "$ZC_HOME" >> "$rc"
      say "added $ZC_HOME/bin to PATH in $rc"
    done
    say "open a new shell, or: export PATH=\"$ZC_HOME/bin:\$PATH\""
    ;;
esac

# ---- 4. OTP runtime (no Docker, no system erlang) ----
if [ -z "${ZC_SKIP_OTP:-}" ]; then
  say "provisioning the OTP runtime (zc toolchain install) ..."
  "$ZC_HOME/bin/zc" toolchain install || say "OTP install skipped/failed; run 'zc toolchain install' later"
fi

say "done. Try:  zc init hello && cd hello && zc run"
