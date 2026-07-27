#!/usr/bin/env bash
# gen-repo-key.sh — generate the GPG key that signs ALL repositories on the
# mirror (one shared key for every project: apt, yum, future projects).
#
# Creates an RSA-4096 key "Trusted Technologies Repo <repo@trust-me.host>"
# (no passphrase, never expires) in your default GPG home directory, exports
# the public key to ./trusted-technologies.asc and prints the fingerprint.
#
# Afterwards:
#   export NEST_GPG_KEY=<fingerprint>        # for make-apt-repo.sh / make-yum-repo.sh
#   gpg --export-secret-keys --armor <fpr>   # store as the CI secret GPG_PRIVATE_KEY
#   the public key is published on the mirror as /keys/trusted-technologies.asc
#   (scripts/make-apt-repo.sh re-exports it to dist/repo/keys/)
set -euo pipefail
cd "$(dirname "$0")/.."

NAME="Trusted Technologies Repo"
EMAIL="repo@trust-me.host"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/params" <<EOF
Key-Type: RSA
Key-Length: 4096
Subkey-Type: RSA
Subkey-Length: 4096
Name-Real: $NAME
Name-Email: $EMAIL
Expire-Date: 0
%no-protection
%commit
EOF

echo ">> generating RSA-4096 key: $NAME <$EMAIL>"
gpg --batch --generate-key "$tmp/params"

fpr="$(gpg --list-secret-keys --with-colons "$EMAIL" | awk -F: '/^fpr:/ { print $10; exit }')"
[ -n "$fpr" ] || { echo "error: key generation failed" >&2; exit 1; }

gpg --batch --yes --armor --export "$fpr" > trusted-technologies.asc

echo ""
echo "Key generated: $NAME <$EMAIL>"
echo "Fingerprint:   $fpr"
echo "Public key:    $(pwd)/trusted-technologies.asc"
echo ""
echo "Next steps:"
echo "  export NEST_GPG_KEY=$fpr"
echo "  gpg --export-secret-keys --armor $fpr   # -> CI secret GPG_PRIVATE_KEY"
