#!/usr/bin/env bash
# fetch-tools.sh — download the restic and rclone helper binaries that the
# mirror serves under /bin (nest downloads them from there on demand).
#
# Output:
#   dist/tools/bin/restic-linux-{amd64,arm64,armhf}
#   dist/tools/bin/rclone-linux-{amd64,arm64,armhf}
#   dist/tools/bin/sha256sums.txt
#   dist/tools/bin/sha256sums.txt.asc   (detached signature, when NEST_GPG_KEY is set)
#
# Versions are PINNED below — bump them consciously. Every upstream artifact
# is verified against the upstream project's own SHA256SUMS before it is
# republished: nest servers run these helpers as root, so a corrupted or
# tampered upstream download must never reach the mirror.
#
# Upstream arch names differ: armhf is "arm" for restic and "arm-v7" for rclone.
#
# sha256sums.txt covers every file that lands in the mirror's /bin prefix:
# the helper tools plus dist/bin/nest-linux-* when those are already built
# (so run scripts/build.sh before this script). Entries use bare filenames.
# nest clients verify this file's signature (.asc) against the repository
# public key embedded in the nest binary — sign it whenever publishing.
#
# Environment:
#   NEST_GPG_KEY    key id/fingerprint used to sign sha256sums.txt
#                   (optional locally, REQUIRED for publishing)
#   RESTIC_VERSION  override the pinned restic version (no "v" prefix)
#   RCLONE_VERSION  override the pinned rclone version (no "v" prefix)
#
# Requires: curl, bunzip2 (bzip2), unzip, sha256sum; gpg when signing.
set -euo pipefail
cd "$(dirname "$0")/.."

RESTIC_VERSION="${RESTIC_VERSION:-0.19.1}"
RCLONE_VERSION="${RCLONE_VERSION:-1.74.4}"

out=dist/tools/bin
mkdir -p "$out"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

restic_arch() { case "$1" in amd64|arm64) echo "$1" ;; armhf) echo arm ;; esac; }
rclone_arch() { case "$1" in amd64|arm64) echo "$1" ;; armhf) echo arm-v7 ;; esac; }

# verify <file> <expected-name-in-sums> <sums-file>
verify() {
    local file="$1" name="$2" sums="$3"
    local want got
    want="$(awk -v n="$name" '$2 == n || $2 == "*"n { print $1 }' "$sums")"
    [ -n "$want" ] || { echo "error: no upstream checksum for $name" >&2; exit 1; }
    got="$(sha256sum "$file" | cut -d' ' -f1)"
    [ "$got" = "$want" ] || { echo "error: upstream checksum mismatch for $name" >&2; exit 1; }
    echo "   upstream checksum OK: $name"
}

# --- upstream checksum manifests ----------------------------------------------
echo ">> fetching upstream checksums (restic $RESTIC_VERSION, rclone $RCLONE_VERSION)"
curl -fsSL -o "$tmp/restic-SHA256SUMS" \
    "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/SHA256SUMS"
curl -fsSL -o "$tmp/rclone-SHA256SUMS" \
    "https://github.com/rclone/rclone/releases/download/v${RCLONE_VERSION}/SHA256SUMS"

for arch in amd64 arm64 armhf; do
    ra="$(restic_arch "$arch")"
    echo ">> restic linux/$arch"
    curl -fsSL -o "$tmp/restic-$arch.bz2" \
        "https://github.com/restic/restic/releases/download/v${RESTIC_VERSION}/restic_${RESTIC_VERSION}_linux_${ra}.bz2"
    verify "$tmp/restic-$arch.bz2" "restic_${RESTIC_VERSION}_linux_${ra}.bz2" "$tmp/restic-SHA256SUMS"
    bunzip2 -c "$tmp/restic-$arch.bz2" > "$out/restic-linux-$arch"
    chmod 0755 "$out/restic-linux-$arch"

    ka="$(rclone_arch "$arch")"
    echo ">> rclone linux/$arch"
    curl -fsSL -o "$tmp/rclone-$arch.zip" \
        "https://downloads.rclone.org/v${RCLONE_VERSION}/rclone-v${RCLONE_VERSION}-linux-${ka}.zip"
    verify "$tmp/rclone-$arch.zip" "rclone-v${RCLONE_VERSION}-linux-${ka}.zip" "$tmp/rclone-SHA256SUMS"
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

# --- sign the checksum manifest ------------------------------------------------
# Clients (nest self-update / deps.Ensure) refuse unsigned sums, so a build
# meant for publishing MUST set NEST_GPG_KEY.
rm -f "$sums.asc"
if [ -n "${NEST_GPG_KEY:-}" ]; then
    echo ">> signing sha256sums.txt with key $NEST_GPG_KEY"
    gpg --batch --yes --armor --local-user "$NEST_GPG_KEY" \
        --output "$sums.asc" --detach-sign "$sums"
else
    echo ">> WARNING: NEST_GPG_KEY not set — sha256sums.txt left unsigned (dev build only)"
fi

echo ">> done:"
ls -l "$out"
