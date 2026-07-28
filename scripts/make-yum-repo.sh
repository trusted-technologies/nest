#!/usr/bin/env bash
# make-yum-repo.sh — build the yum/dnf repository served at /yum on the mirror.
#
# Input:  dist/packages/*.rpm   (run scripts/package.sh first)
# Output: dist/repo/yum/
#           *.rpm
#           repodata/ (createrepo_c) + repodata/repomd.xml.asc (detached signature)
#
# The repository is a single multi-arch directory; the .rpm files carry
# x86_64/aarch64 payloads and yum/dnf pick the right one.
#
# Requires: createrepo_c, gpg
#   Debian/Ubuntu: apt-get install createrepo-c gnupg
#   Fedora/RHEL:   dnf install createrepo_c gnupg2
# Environment:
#   NEST_GPG_KEY — key id/fingerprint used to sign repomd.xml (required).
#     If `rpmsign` is available the .rpm packages themselves are signed too.
set -euo pipefail
cd "$(dirname "$0")/.."

: "${NEST_GPG_KEY:?set NEST_GPG_KEY to the GPG key id/fingerprint used to sign the yum repo}"
command -v createrepo_c >/dev/null 2>&1 || { echo "error: createrepo_c missing (apt-get install createrepo-c)" >&2; exit 1; }

repo=dist/repo/yum
rm -rf "$repo"
mkdir -p "$repo"
cp dist/packages/*.rpm "$repo/"

# The packages themselves must be signed too: clients run with gpgcheck=1,
# and publishing unsigned RPMs would either break installations or tempt
# admins into disabling signature checks. Never skip silently.
if ! command -v rpmsign >/dev/null 2>&1; then
    echo "error: rpmsign not found (Debian/Ubuntu: apt-get install rpm) — refusing to publish unsigned RPMs" >&2
    exit 1
fi
echo ">> signing rpms with key $NEST_GPG_KEY"
# Some rpm builds resolve %__gpg to a bare "gpg" and exec it without a
# PATH lookup — always pass the absolute path.
rpmsign --addsign \
    -D "__gpg $(command -v gpg)" \
    -D "_gpg_name $NEST_GPG_KEY" \
    "$repo"/*.rpm

echo ">> createrepo_c"
createrepo_c "$repo"

echo ">> signing repodata/repomd.xml"
gpg --batch --yes --armor --local-user "$NEST_GPG_KEY" \
    --output "$repo/repodata/repomd.xml.asc" \
    --detach-sign "$repo/repodata/repomd.xml"

# zypper looks for the repo key at repodata/repomd.xml.key.
echo ">> exporting key to repodata/repomd.xml.key"
gpg --batch --yes --armor --export "$NEST_GPG_KEY" > "$repo/repodata/repomd.xml.key"

echo ">> yum repo ready at $repo"
