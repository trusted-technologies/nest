#!/usr/bin/env bash
# build.sh — build static nest binaries for all supported architectures.
#
# Output (run from the repository root):
#   dist/bin/nest-linux-amd64
#   dist/bin/nest-linux-arm64
#   dist/bin/nest-linux-armhf   (GOARCH=arm, GOARM=7)
#
# Version: if HEAD is exactly on a git tag, the tag (without leading "v")
# is used; in CI the workflow writes an auto version (1.0.<run-number>)
# into ./VERSION before building; local untagged builds fall back to "dev".
# The version is injected into the binary via -ldflags -X .../buildinfo.Version.
#
# Toolchain: `go` from PATH, the $GO env variable, or ./.tools/go/bin/go
# (in that order). CGO is disabled; binaries are fully static.
set -euo pipefail
cd "$(dirname "$0")/.."

# --- locate go ---------------------------------------------------------------
if [ -n "${GO:-}" ] && command -v "${GO%% *}" >/dev/null 2>&1; then
    go_bin="$GO"
elif command -v go >/dev/null 2>&1; then
    go_bin=go
elif [ -x ./.tools/go/bin/go ]; then
    go_bin=./.tools/go/bin/go
    # repo-local caches used by the bundled toolchain setup
    export GOCACHE="${GOCACHE:-$PWD/.tools/gocache}"
    export GOMODCACHE="${GOMODCACHE:-$PWD/.tools/gomodcache}"
    export GOPATH="${GOPATH:-$PWD/.tools/gopath}"
else
    echo "error: no go toolchain found (looked in \$GO, PATH, ./.tools/go/bin/go)" >&2
    exit 1
fi

# --- resolve version ---------------------------------------------------------
VERSION=""
if [ -d .git ] && command -v git >/dev/null 2>&1; then
    if tag="$(git describe --exact-match --tags HEAD 2>/dev/null)"; then
        VERSION="${tag#v}"
    fi
fi
if [ -z "$VERSION" ] && [ -f VERSION ]; then
    VERSION="$(tr -d '[:space:]' < VERSION)"
fi
VERSION="${VERSION:-dev}"
echo ">> nest version $VERSION ($go_bin)"

# --- build -------------------------------------------------------------------
LDFLAGS="-s -w -X github.com/trusted-technologies/nest/internal/buildinfo.Version=$VERSION"
mkdir -p dist/bin

build() { # goarch goarm suffix
    echo ">> building nest-linux-$3"
    env CGO_ENABLED=0 GOOS=linux GOARCH="$1" ${2:+GOARM="$2"} \
        "$go_bin" build -trimpath -ldflags "$LDFLAGS" \
        -o "dist/bin/nest-linux-$3" ./cmd/nest
}

build amd64 "" amd64
build arm64 "" arm64
build arm    7  armhf

echo ">> done:"
ls -l dist/bin/
