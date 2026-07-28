#!/usr/bin/env bash
# test-e2e.sh — end-to-end test of the full distribution pipeline with docker.
#
#   1. Builds the apt + yum repositories inside a debian:12 container with a
#      throwaway GPG key (nothing touches your real gpg home).
#   2. Stages a local mirror (dist/mirror/) exactly like scripts/publish.sh
#      would upload it, and serves it over HTTP on a docker network.
#   3. Boots one container per supported distro, runs the public install
#      one-liner against the local mirror and verifies nest works.
#
# Usage:  bash scripts/test-e2e.sh [image ...]
# Default images: debian:10 debian:12 ubuntu:16.04 ubuntu:22.04 rockylinux:9 almalinux:8 amazonlinux:2023 opensuse/leap:15.6
#
# Requires: docker, built packages (scripts/build.sh + scripts/package.sh).
set -euo pipefail
cd "$(dirname "$0")/.."

NET="nest-e2e"
MIRROR_CTR="nest-e2e-mirror"
PORT=8080
# Some hosts advertise unreachable (VPN/LAN) DNS servers into containers;
# force public resolvers so apt/dnf/zypper work inside the test containers.
DNS=(--dns 1.1.1.1 --dns 8.8.8.8)

IMAGES=("$@")
if [ "${#IMAGES[@]}" -eq 0 ]; then
    IMAGES=(debian:10 debian:12 ubuntu:16.04 ubuntu:22.04 rockylinux:9 almalinux:8 amazonlinux:2023 opensuse/leap:15.6)
fi

ls dist/packages/*.deb dist/packages/*.rpm >/dev/null 2>&1 || {
    echo "error: dist/packages is empty — run scripts/build.sh && scripts/package.sh first" >&2
    exit 1
}

cleanup() {
    docker rm -f "$MIRROR_CTR" >/dev/null 2>&1 || true
    docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

docker network create "$NET" >/dev/null

# --- stage 1+2: build repos, stage and serve the mirror -----------------------
echo "== building repositories and starting local mirror =="
# The repo is mounted READ-ONLY; the container works on a private copy, so
# test artifacts (throwaway key material, staged mirror, root-owned files)
# never land in the host working tree.
docker run -d "${DNS[@]}" --name "$MIRROR_CTR" --network "$NET" --network-alias mirror \
    -v "$PWD:/nest-ro:ro" debian:12 sleep infinity >/dev/null

docker exec "$MIRROR_CTR" bash -c '
    set -euo pipefail
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq dpkg-dev apt-utils createrepo-c rpm gnupg python3 >/dev/null

    mkdir /work
    tar -C /nest-ro --exclude=./.git --exclude=./.tools -cf - . | tar -C /work -xf -
    cd /work

    export GNUPGHOME=/tmp/gnupg
    mkdir -p "$GNUPGHOME" && chmod 700 "$GNUPGHOME"
    fpr="$(bash scripts/gen-repo-key.sh | awk "/^Fingerprint:/ { print \$2 }")"
    [ -n "$fpr" ] || { echo "key generation failed" >&2; exit 1; }
    export NEST_GPG_KEY="$fpr"

    bash scripts/make-apt-repo.sh
    bash scripts/make-yum-repo.sh

    m=dist/mirror
    rm -rf "$m" && mkdir -p "$m/nest/bin" "$m/keys"
    cp scripts/install.sh "$m/nest/"
    if [ -f VERSION ]; then cp VERSION "$m/nest/"; else echo "0.0.0-e2e" > "$m/nest/VERSION"; fi
    cp -r dist/repo/apt dist/repo/yum "$m/nest/"
    cp dist/bin/* "$m/nest/bin/"
    (cd "$m/nest/bin" && sha256sum nest-linux-* > sha256sums.txt)
    gpg --batch --yes --armor --local-user "$NEST_GPG_KEY" \
        --detach-sign --output "$m/nest/bin/sha256sums.txt.asc" "$m/nest/bin/sha256sums.txt"
    cp dist/repo/keys/trusted-technologies.* "$m/keys/"
    printf %s "$fpr" > "$m/nest/key.fpr"
'

docker exec -d "$MIRROR_CTR" python3 -m http.server "$PORT" --directory /work/dist/mirror

# wait for the server
for i in $(seq 1 30); do
    if docker exec "$MIRROR_CTR" bash -c "exec 3<>/dev/tcp/127.0.0.1/$PORT" 2>/dev/null; then
        break
    fi
    sleep 1
done
echo "== mirror is up (container $MIRROR_CTR, network $NET) =="

# --- stage 3: per-distro install test ------------------------------------------
read -r -d '' TEST_BODY <<'EOF' || true
set -e
BASE="http://mirror:8080/nest"
KEYS_BASE="http://mirror:8080/keys"
# EOL distros: base images ship dead apt URLs, point them at the archives.
. /etc/os-release 2>/dev/null || true
case "${ID:-}:${VERSION_ID:-}" in
    debian:10)
        sed -i 's|deb\.debian\.org|archive.debian.org|g; s|security\.debian\.org|archive.debian.org|g' /etc/apt/sources.list ;;
    ubuntu:16.04)
        sed -i 's|archive\.ubuntu\.com|old-releases.ubuntu.com|g; s|security\.ubuntu\.com|old-releases.ubuntu.com|g' /etc/apt/sources.list ;;
esac
if command -v apt-get >/dev/null 2>&1; then
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq >/dev/null 2>&1 || true
    apt-get install -y -qq curl ca-certificates >/dev/null 2>&1 || true
elif command -v zypper >/dev/null 2>&1; then
    zypper --non-interactive install -y curl >/dev/null 2>&1 || true
fi
NEST_MIRROR_BASE="$BASE" NEST_KEYS_BASE="$KEYS_BASE" NEST_KEY_FPR="$(curl -fsSL "$BASE/key.fpr")" bash <(curl -fsSL "$BASE/install.sh")
echo "-- nest version: $(nest version)"
nest help | head -2
if command -v dpkg >/dev/null 2>&1; then
    dpkg -s nest | grep -E "^(Package|Version|Architecture):"
    pkg_files="dpkg -L nest"
else
    rpm -q nest
    pkg_files="rpm -ql nest"
fi
eval "$pkg_files" | grep -E "nest\.(service|timer)$"
test -d /etc/nest && test -d /var/lib/nest
echo "E2E-PASS"
EOF

# ubuntu:16.04 is EOL and gone from all public apt mirrors, so curl cannot be
# installed from its repos. Mount a static curl in instead — the thing being
# tested is apt 1.2's signed-by + InRelease verification, not xenial's repos.
for img in "${IMAGES[@]}"; do
    if [ "$img" = "ubuntu:16.04" ] && [ ! -x .tools/curl-amd64 ]; then
        echo "== fetching static curl for the xenial test =="
        mkdir -p .tools
        curl -fsSL -o .tools/curl-amd64 \
            https://github.com/moparisthebest/static-curl/releases/download/v8.21.0/curl-amd64
        chmod +x .tools/curl-amd64
    fi
done

failed=0
for img in "${IMAGES[@]}"; do
    echo "== $img =="
    EXTRA_ARGS=()
    if [ "$img" = "ubuntu:16.04" ]; then
        EXTRA_ARGS=(-v "$PWD/.tools/curl-amd64:/usr/bin/curl:ro")
    fi
    if ! docker pull -q "$img" >/dev/null 2>&1; then
        echo "   SKIP (image pull failed)"
        continue
    fi
    out="$(docker run --rm "${DNS[@]}" "${EXTRA_ARGS[@]}" --network "$NET" "$img" bash -c "$TEST_BODY" 2>&1)" && rc=0 || rc=$?
    echo "$out" | tail -8
    if [ "$rc" -eq 0 ] && printf '%s' "$out" | grep -q "E2E-PASS"; then
        echo "   PASS"
    else
        echo "   FAIL"
        failed=1
    fi
done

exit "$failed"
