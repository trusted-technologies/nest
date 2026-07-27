#!/usr/bin/env bash
# publish.sh — upload the release artifacts to the Cloudflare R2 mirror
# (public base https://mirror.trust-me.host).
#
# The mirror is shared between projects; nest lives under the /nest prefix
# and the repository signing key (shared by all projects) under /keys:
#
#   scripts/install.sh                    -> /nest/install.sh
#   VERSION                               -> /nest/VERSION
#   dist/bin/nest-linux-*                 -> /nest/bin/   \
#   dist/tools/bin/*                      -> /nest/bin/    } synced with --delete
#   dist/repo/apt                         -> /nest/apt/    }
#   dist/repo/yum                         -> /nest/yum/   /
#   dist/repo/keys/trusted-technologies.* -> /keys/       (copied, never deleted)
#
# --delete is used ONLY under /nest/{bin,apt,yum}, so stale artifacts are
# removed there. Everything else in the bucket (other projects, /keys) is
# never touched.
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

s3() { aws --endpoint-url "$R2_ENDPOINT" s3 "$@"; }

# --- stage /nest/bin: nest binaries + helper tools into one tree --------------
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT
mkdir -p "$staging/bin"
cp dist/bin/nest-linux-* "$staging/bin/"
cp dist/tools/bin/* "$staging/bin/"

echo ">> syncing /nest/{bin,apt,yum} (with --delete)"
s3 sync "$staging/bin"  "s3://$R2_BUCKET/nest/bin" --delete
s3 sync dist/repo/apt   "s3://$R2_BUCKET/nest/apt" --delete
s3 sync dist/repo/yum   "s3://$R2_BUCKET/nest/yum" --delete

echo ">> uploading top-level files"
s3 cp scripts/install.sh "s3://$R2_BUCKET/nest/install.sh" --content-type "text/plain; charset=utf-8"
s3 cp VERSION            "s3://$R2_BUCKET/nest/VERSION"

echo ">> uploading the shared repo signing key to /keys"
s3 cp dist/repo/keys/trusted-technologies.asc "s3://$R2_BUCKET/keys/trusted-technologies.asc" --content-type "text/plain; charset=utf-8"
s3 cp dist/repo/keys/trusted-technologies.gpg "s3://$R2_BUCKET/keys/trusted-technologies.gpg"

echo ""
echo "Published:"
echo "  $BASE/install.sh"
echo "  $BASE/VERSION"
echo "  $BASE/bin/   (nest, restic, rclone binaries + sha256sums.txt)"
echo "  $BASE/apt/   (Debian/Ubuntu repository)"
echo "  $BASE/yum/   (RPM repository)"
echo "  ${BASE%/nest}/keys/trusted-technologies.{asc,gpg}   (shared repo key)"
