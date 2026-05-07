#!/usr/bin/env bash
# vendor/setup-libgc.sh — locate the system's bdwgc shared library and
# create the unversioned libgc.so symlink the Zig linker expects. Saves
# users from needing to install the gc-devel / libgc-dev package.

set -e
DEST="$(cd "$(dirname "$0")" && pwd)/lib"
mkdir -p "$DEST"

for candidate in \
    /lib64/libgc.so.1 \
    /lib/x86_64-linux-gnu/libgc.so.1 \
    /usr/lib64/libgc.so.1 \
    /usr/lib/libgc.so.1 \
    /usr/local/lib/libgc.so.1 \
    /opt/homebrew/lib/libgc.so.1 \
    /opt/homebrew/lib/libgc.dylib \
    /usr/local/lib/libgc.dylib
do
    if [ -e "$candidate" ]; then
        ln -sf "$candidate" "$DEST/libgc.so"
        echo "linked $candidate -> $DEST/libgc.so"
        exit 0
    fi
done

echo "error: libgc shared library not found" >&2
echo "install bdwgc (libgc-dev / gc-devel / brew install bdw-gc) and re-run" >&2
exit 1
