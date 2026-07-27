#!/usr/bin/env bash
# fetch-tools.sh — download the restic and rclone helper binaries that the
# mirror serves under /bin (nest downloads them from there on demand).
#
# Output:
#   dist/tools/bin/restic-linux-{amd64,arm64,armhf}
#   dist/tools/bin/rclone-linux-{amd64,arm64,armhf}
#   dist/tools/bin/sha256sums.txt
#
# restic: latest GitHub release (.bz2, decompressed with bunzip2).
# rclone: latest stable from downloads.rclone.org (.zip, binary extracted).
# Upstream arch names differ: armhf is "arm" for restic and "arm-v7" for rclone.
#
# sha256sums.txt covers every file that lands in the mirror's /bin prefix:
# the helper tools plus dist/bin/nest-linux-* when those are already built
# (so run scripts/build.sh before this script). Entries use bare filenames.
#
# Requires: curl, bunzip2 (bzip2), unzip, sha256sum.
set -euo pipefail
cd "$(dirname "$0")/.."

out=dist/tools/bin
mkdir -p "$out"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

restic_arch() { case "$1" in amd64|arm64) echo "$1" ;; armhf) echo arm ;; esac; }
rclone_arch() { case "$1" in amd64|arm64) echo "$1" ;; armhf) echo arm-v7 ;; esac; }

# --- restic: resolve latest release tag --------------------------------------
echo ">> resolving latest restic release"
tag="$(curl -fsSL https://api.github.com/repos/restic/restic/releases/latest \
    | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)"
[ -n "$tag" ] || { echo "error: could not determine latest restic version" >&2; exit 1; }
ver="${tag#v}"
echo ">> restic $tag, rclone latest stable"

for arch in amd64 arm64 armhf; do
    ra="$(restic_arch "$arch")"
    echo ">> restic linux/$arch"
    curl -fsSL -o "$tmp/restic-$arch.bz2" \
        "https://github.com/restic/restic/releases/download/${tag}/restic_${ver}_linux_${ra}.bz2"
    bunzip2 -c "$tmp/restic-$arch.bz2" > "$out/restic-linux-$arch"
    chmod 0755 "$out/restic-linux-$arch"

    ka="$(rclone_arch "$arch")"
    echo ">> rclone linux/$arch"
    curl -fsSL -o "$tmp/rclone-$arch.zip" \
        "https://downloads.rclone.org/rclone-current-linux-${ka}.zip"
    unzip -p "$tmp/rclone-$arch.zip" '*/rclone' > "$out/rclone-linux-$arch"
    chmod 0755 "$out/rclone-linux-$arch"
done

# --- checksums for everything published under /bin ----------------------------
echo ">> writing $out/sha256sums.txt"
sums="$out/sha256sums.txt"
: > "$sums"
for f in dist/bin/nest-linux-* "$out"/restic-linux-* "$out"/rclone-linux-*; do
    [ -e "$f" ] || continue
    sha256sum "$f" | sed 's|  \S*/|  |' >> "$sums"
done

echo ">> done:"
ls -l "$out"
