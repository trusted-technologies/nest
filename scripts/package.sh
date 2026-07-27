#!/usr/bin/env bash
# package.sh — build .deb and .rpm packages with nfpm.
#
# Input:  dist/bin/nest-linux-{amd64,arm64,armhf}  (run scripts/build.sh first)
# Output: dist/packages/*.deb, dist/packages/*.rpm
#
# Matrix: amd64 -> deb+rpm, arm64 -> deb+rpm, armhf -> deb only.
# nfpm is looked up in PATH, $GOPATH/bin and ./.tools. ARCH/VERSION/SRC are
# passed through the environment and expanded inside packaging/nfpm.yaml;
# nfpm itself translates the Go-style arch names for rpm (x86_64/aarch64).
set -euo pipefail
cd "$(dirname "$0")/.."

# --- locate nfpm -------------------------------------------------------------
NFPM=""
if command -v nfpm >/dev/null 2>&1; then
    NFPM=nfpm
else
    gopath="$(go env GOPATH 2>/dev/null || true)"
    for candidate in "${gopath:+$gopath/bin/nfpm}" .tools/bin/nfpm .tools/nfpm; do
        if [ -n "$candidate" ] && [ -x "$candidate" ]; then
            NFPM="$candidate"
            break
        fi
    done
fi
[ -n "$NFPM" ] || { echo "error: nfpm not found (PATH, \$GOPATH/bin, .tools)" >&2; exit 1; }

# --- resolve version (same logic as build.sh) --------------------------------
VERSION=""
if [ -d .git ] && command -v git >/dev/null 2>&1; then
    if tag="$(git describe --exact-match --tags HEAD 2>/dev/null)"; then
        VERSION="${tag#v}"
    fi
fi
[ -n "$VERSION" ] || VERSION="$(tr -d '[:space:]' < VERSION)"
export VERSION

# --- package -----------------------------------------------------------------
mkdir -p dist/packages

pack() { # arch formats...
    arch="$1"; shift
    src="dist/bin/nest-linux-$arch"
    [ -f "$src" ] || { echo "error: $src missing — run scripts/build.sh first" >&2; exit 1; }
    for fmt in "$@"; do
        echo ">> nfpm: nest $VERSION linux/$arch -> $fmt"
        tmp_cfg="$(mktemp)"
        sed -e "s|\${ARCH}|$arch|g" \
            -e "s|\${VERSION}|$VERSION|g" \
            -e "s|\${SRC}|$src|g" \
            packaging/nfpm.yaml > "$tmp_cfg"
        "$NFPM" package --config "$tmp_cfg" --packager "$fmt" --target dist/packages/
        rm -f "$tmp_cfg"
    done
}

pack amd64 deb rpm
pack arm64 deb rpm
pack armhf deb

echo ">> done:"
ls -l dist/packages/
