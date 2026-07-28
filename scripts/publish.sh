#!/usr/bin/env bash
# publish.sh — upload the release artifacts to the Cloudflare R2 mirror
# (public base https://mirror.trust-me.host).
#
# The mirror is shared between projects; nest lives under the /nest prefix.
# The shared repository signing key under /keys is NOT uploaded here —
# writing to a cross-project prefix must require a separate, tightly
# guarded process (see mirror.md), so this script never leaves /nest:
#
#   scripts/install.sh                    -> /nest/install.sh
#   dist/VERSION                          -> /nest/VERSION
#   dist/bin/nest-linux-*                 -> /nest/bin/   \
#   dist/tools/bin/*                      -> /nest/bin/    } synced with --delete
#   dist/repo/apt                         -> /nest/apt/    }
#   dist/repo/yum                         -> /nest/yum/   /
#
# --delete is used ONLY under /nest/{bin,apt,yum}, so stale artifacts are
# removed there. Everything else in the bucket (other projects, /keys) is
# never touched.
#
# Cache-Control: the mirror sits behind the Cloudflare CDN, which happily
# serves stale repo metadata (the classic apt "Hash Sum mismatch"). All
# frequently-changing files (repo indices, VERSION, install.sh, checksums,
# nest binaries) are uploaded with no-cache; only big immutable artifacts
# (.deb/.rpm in pool/, helper binaries) get a long cache lifetime.
#
# Required environment:
#   R2_ENDPOINT            e.g. https://<account-id>.r2.cloudflarestorage.com
#   R2_BUCKET              bucket name
#   AWS_ACCESS_KEY_ID      R2 access key id
#   AWS_SECRET_ACCESS_KEY  R2 secret access key
# Optional:
#   NEST_MIRROR_BASE       public project base URL for the summary
#                          (default https://mirror.trust-me.host/nest)
#
# Requires: aws CLI v2 (or v1) configured for S3.
set -euo pipefail
cd "$(dirname "$0")/.."

for var in R2_ENDPOINT R2_BUCKET AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY; do
    if [ -z "${!var:-}" ]; then
        echo "error: \$$var is not set (need R2_ENDPOINT, R2_BUCKET, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY)" >&2
        exit 1
    fi
done
export AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
BASE="${NEST_MIRROR_BASE:-https://mirror.trust-me.host/nest}"

NOCACHE="no-cache"                 # revalidate every request
LONGCACHE="public, max-age=86400"  # immutable content, cache for a day

s3() { aws --endpoint-url "$R2_ENDPOINT" s3 "$@"; }

# --- stage /nest/bin: nest binaries + helper tools into one tree --------------
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT
mkdir -p "$staging/bin"
cp dist/bin/nest-linux-* "$staging/bin/"
cp dist/tools/bin/* "$staging/bin/"

# Version file: CI writes the resolved version into ./VERSION before the
# build (auto 1.0.<run-number>, or the tag). Local dev publishes usually
# have no VERSION file — then /nest/VERSION on the mirror is left as-is,
# so servers are not offered a dev binary via self-update.
VERSION_FILE=""
[ -f VERSION ] && VERSION_FILE=VERSION
if [ -z "$VERSION_FILE" ]; then
    echo ">> warning: no VERSION file (dev publish) — /nest/VERSION stays unchanged"
fi

echo ">> syncing /nest/{bin,apt,yum} (with --delete)"
s3 sync "$staging/bin"  "s3://$R2_BUCKET/nest/bin" --delete \
    --exclude "restic-*" --exclude "rclone-*" --exclude "sha256sums.txt*" \
    --cache-control "$NOCACHE"
s3 sync "$staging/bin"  "s3://$R2_BUCKET/nest/bin" \
    --exclude "*" --include "restic-*" --include "rclone-*" \
    --cache-control "$LONGCACHE"
# The checksum manifest and its signature: clients verify fail-closed, so
# both must always be present and fresh. Refuse to publish unsigned sums.
[ -f "$staging/bin/sha256sums.txt.asc" ] || {
    echo "error: sha256sums.txt.asc missing — rebuild tools with NEST_GPG_KEY set" >&2
    exit 1
}
s3 cp "$staging/bin/sha256sums.txt" "s3://$R2_BUCKET/nest/bin/sha256sums.txt" \
    --cache-control "$NOCACHE"
s3 cp "$staging/bin/sha256sums.txt.asc" "s3://$R2_BUCKET/nest/bin/sha256sums.txt.asc" \
    --cache-control "$NOCACHE"

# apt: pool (immutable .deb) long cache, dists (indices) no-cache
s3 sync dist/repo/apt/pool  "s3://$R2_BUCKET/nest/apt/pool" --delete \
    --cache-control "$LONGCACHE"
s3 sync dist/repo/apt/dists "s3://$R2_BUCKET/nest/apt/dists" --delete \
    --cache-control "$NOCACHE"

# yum: rpms (immutable) long cache, repodata no-cache
s3 sync dist/repo/yum "s3://$R2_BUCKET/nest/yum" --delete \
    --exclude "repodata/*" --cache-control "$LONGCACHE"
s3 sync dist/repo/yum/repodata "s3://$R2_BUCKET/nest/yum/repodata" --delete \
    --cache-control "$NOCACHE"

echo ">> uploading top-level files"
s3 cp scripts/install.sh "s3://$R2_BUCKET/nest/install.sh" \
    --content-type "text/plain; charset=utf-8" --cache-control "$NOCACHE"
if [ -n "$VERSION_FILE" ]; then
    s3 cp "$VERSION_FILE" "s3://$R2_BUCKET/nest/VERSION" --cache-control "$NOCACHE"
fi

echo ""
echo "Published:"
echo "  $BASE/install.sh"
echo "  $BASE/VERSION"
echo "  $BASE/bin/   (nest, restic, rclone binaries + signed sha256sums.txt)"
echo "  $BASE/apt/   (Debian/Ubuntu repository)"
echo "  $BASE/yum/   (RPM repository)"
