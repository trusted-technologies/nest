#!/usr/bin/env bash
# make-apt-repo.sh — build the apt repository served at /apt on the mirror.
#
# Input:  dist/packages/*.deb            (run scripts/package.sh first)
# Output: dist/repo/apt/
#           pool/main/n/nest/*.deb
#           dists/stable/main/binary-{amd64,arm64,armhf}/Packages[.gz]
#           dists/stable/{Release,InRelease,Release.gpg}
#         dist/repo/keys/trusted-technologies.{asc,gpg}   (repo signing key)
#
# Requires: dpkg-scanpackages, apt-ftparchive, gpg
#   Debian/Ubuntu: apt-get install dpkg-dev apt-utils gnupg
# Environment:
#   NEST_GPG_KEY — key id or fingerprint used to sign the repo (required).
set -euo pipefail
cd "$(dirname "$0")/.."

: "${NEST_GPG_KEY:?set NEST_GPG_KEY to the GPG key id/fingerprint used to sign the apt repo}"
command -v dpkg-scanpackages >/dev/null 2>&1 || { echo "error: dpkg-scanpackages missing (apt-get install dpkg-dev)" >&2; exit 1; }
command -v apt-ftparchive   >/dev/null 2>&1 || { echo "error: apt-ftparchive missing (apt-get install apt-utils)" >&2; exit 1; }

repo=dist/repo/apt
rm -rf "$repo"
mkdir -p "$repo/pool/main/n/nest"
cp dist/packages/*.deb "$repo/pool/main/n/nest/"

cd "$repo"

# --- per-arch Packages indices ------------------------------------------------
for arch in amd64 arm64 armhf; do
    dir="dists/stable/main/binary-$arch"
    mkdir -p "$dir"
    echo ">> scanning pool for $arch"
    # An arch without packages yields an empty (but valid) index.
    dpkg-scanpackages --arch "$arch" pool/main > "$dir/Packages"
    gzip -9c "$dir/Packages" > "$dir/Packages.gz"
done

# --- Release metadata ----------------------------------------------------------
conf="$(mktemp)"
trap 'rm -f "$conf"' EXIT
cat > "$conf" <<'EOF'
APT::FTPArchive::Release::Origin "nest";
APT::FTPArchive::Release::Label "nest";
APT::FTPArchive::Release::Suite "stable";
APT::FTPArchive::Release::Codename "stable";
APT::FTPArchive::Release::Architectures "amd64 arm64 armhf";
APT::FTPArchive::Release::Components "main";
APT::FTPArchive::Release::Description "nest apt repository";
EOF

echo ">> generating Release"
apt-ftparchive -c "$conf" release dists/stable > dists/stable/Release

# Valid-Until bounds replay/freeze attacks: without it an attacker with
# write access to the mirror (no signing key) could roll clients back to an
# old, still-validly-signed Release and hide security updates forever.
# 4 weeks covers the push-to-publish cadence with headroom; every publish
# refreshes it.
valid_until="$(date -u -d '+28 days' '+%a, %d %b %Y %H:%M:%S UTC')"
echo "Valid-Until: $valid_until" >> dists/stable/Release

# --- sign ----------------------------------------------------------------------
echo ">> signing with key $NEST_GPG_KEY"
gpg --batch --yes --local-user "$NEST_GPG_KEY" \
    --output dists/stable/InRelease --clearsign dists/stable/Release
gpg --batch --yes --local-user "$NEST_GPG_KEY" \
    --output dists/stable/Release.gpg --detach-sign dists/stable/Release

# --- export the public key (published under /keys on the mirror) --------------
mkdir -p ../keys
gpg --batch --yes --armor --export "$NEST_GPG_KEY" > ../keys/trusted-technologies.asc
# Pre-dearmored copy for minimal systems without a gpg binary.
gpg --batch --yes --export "$NEST_GPG_KEY" > ../keys/trusted-technologies.gpg

# --- self-check: the exported key MUST validate what we just signed -----------
# Catches "signed with the wrong key" (bad NEST_GPG_KEY) before clients do.
echo ">> verifying signatures against the exported key"
gnupg_tmp="$(mktemp -d)"
trap 'rm -f "$conf"; rm -rf "$gnupg_tmp"' EXIT
export GNUPGHOME="$gnupg_tmp"
gpg --batch --import ../keys/trusted-technologies.asc 2>/dev/null
gpg --batch --verify dists/stable/InRelease 2>&1 | grep -q "Good signature" \
    || { echo "error: InRelease does not verify against the exported key" >&2; exit 1; }
gpg --batch --verify dists/stable/Release.gpg dists/stable/Release 2>&1 | grep -q "Good signature" \
    || { echo "error: Release.gpg does not verify against the exported key" >&2; exit 1; }

echo ">> apt repo ready at $repo"
