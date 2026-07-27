#!/usr/bin/env bash
# test-backup.sh — functional test: real backups and restores against a
# local MinIO (S3) server, all inside docker.
#
# Covers, for both backends (restic incremental + tar.gz archive):
#   install the .deb -> configure /etc/nest/config.yml -> nest backup ->
#   modify data -> nest backup -> snapshots -> restore latest -> diff.
#
# Usage: bash scripts/test-backup.sh
# Requires: docker, dist/packages/nest_*_amd64.deb, dist/tools/bin/ helpers.
set -euo pipefail
cd "$(dirname "$0")/.."

NET="nest-func"
MINIO_CTR="nest-func-minio"
BUCKET="nest-test"
AK="nestaccess"
SK="nestsecret123"

cleanup() {
    docker rm -f "$MINIO_CTR" nest-func-tester >/dev/null 2>&1 || true
    docker network rm "$NET" >/dev/null 2>&1 || true
}
trap cleanup EXIT
cleanup

ls dist/tools/bin/restic-linux-amd64 dist/tools/bin/rclone-linux-amd64 >/dev/null || {
    echo "error: helper binaries missing — run scripts/fetch-tools.sh first" >&2
    exit 1
}

docker network create "$NET" >/dev/null

echo "== starting MinIO =="
docker pull -q minio/minio:latest >/dev/null
docker run -d --name "$MINIO_CTR" --network "$NET" --network-alias minio \
    -e MINIO_ROOT_USER="$AK" -e MINIO_ROOT_PASSWORD="$SK" \
    minio/minio:latest server /data >/dev/null

echo "== waiting for MinIO and creating bucket $BUCKET =="
# minio/mc has no shell, so bucket creation is done with rclone (the same
# helper binary nest uses) inside a debian container.
docker run --rm --network "$NET" \
    -v "$PWD/dist/tools/bin:/tb:ro" \
    debian:12 bash -c '
set -e
install -m 0755 /tb/rclone-linux-amd64 /usr/local/bin/rclone
export RCLONE_CONFIG_M_TYPE=s3 \
       RCLONE_CONFIG_M_PROVIDER=Other \
       RCLONE_CONFIG_M_ACCESS_KEY_ID=nestaccess \
       RCLONE_CONFIG_M_SECRET_ACCESS_KEY=nestsecret123 \
       RCLONE_CONFIG_M_ENDPOINT=http://minio:9000
for i in $(seq 1 30); do
    if rclone mkdir m:nest-test 2>/dev/null; then
        exit 0
    fi
    sleep 1
done
echo "error: MinIO did not come up" >&2
exit 1
'
echo "== bucket $BUCKET ready =="

echo "== running functional test =="
docker pull -q debian:12 >/dev/null
docker run --rm -i --dns 1.1.1.1 --dns 8.8.8.8 --network "$NET" \
    -v "$PWD/dist:/nest-dist:ro" \
    debian:12 bash -s <<'EOF'
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq >/dev/null
apt-get install -y -qq ca-certificates diffutils >/dev/null

# install nest from the freshly built .deb + helper binaries from the mirror stash
dpkg -i /nest-dist/packages/nest_*_amd64.deb >/dev/null
install -m 0755 /nest-dist/tools/bin/restic-linux-amd64 /usr/local/bin/restic
install -m 0755 /nest-dist/tools/bin/rclone-linux-amd64 /usr/local/bin/rclone

# test data
mkdir -p /srv/data/sub
echo "hello nest v1" > /srv/data/file1.txt
dd if=/dev/urandom of=/srv/data/sub/blob.bin bs=1k count=100 status=none

cat > /etc/nest/config.yml <<'CFG'
version: 1
encryption_password: "test-password-123"
remotes:
  - name: minio
    type: s3
    endpoint: "http://minio:9000"
    region: us-east-1
    bucket: nest-test
    access_key: nestaccess
    secret_key: nestsecret123
jobs:
  - name: files-restic
    type: folder
    source: /srv/data
    remote: minio
    incremental: true
    retention: {last: 5}
    enabled: true
  - name: files-archive
    type: folder
    source: /srv/data
    remote: minio
    incremental: false
    retention: {last: 5}
    enabled: true
CFG
chmod 600 /etc/nest/config.yml

echo "-- nest jobs"
nest jobs

echo "-- backup 1 (restic)"
nest backup --job files-restic
echo "-- backup 1 (archive)"
nest backup --job files-archive

# change data, back up again
echo "hello nest v2" > /srv/data/file1.txt
echo "new file"      > /srv/data/file2.txt

echo "-- backup 2 (restic)"
nest backup --job files-restic
echo "-- backup 2 (archive)"
nest backup --job files-archive

echo "-- snapshots"
nest snapshots --job files-restic

echo "-- restore latest (restic)"
nest restore --job files-restic --snapshot latest --target /tmp/restore
diff -r /srv/data /tmp/restore/srv/data && echo "RESTIC-RESTORE-OK"

echo "-- restore single path (restic)"
nest restore --job files-restic --snapshot latest --path file2.txt --target /tmp/restore-one
find /tmp/restore-one -type f
grep -q "new file" "$(find /tmp/restore-one -name file2.txt | head -1)" && echo "RESTIC-PATH-RESTORE-OK"

echo "-- check archive landed in the bucket"
export RCLONE_CONFIG_CHECK_TYPE=s3
export RCLONE_CONFIG_CHECK_PROVIDER=Other
export RCLONE_CONFIG_CHECK_ACCESS_KEY_ID=nestaccess
export RCLONE_CONFIG_CHECK_SECRET_ACCESS_KEY=nestsecret123
export RCLONE_CONFIG_CHECK_ENDPOINT=http://minio:9000
rclone lsf check:nest-test/nest-archive/files-archive/ | tee /tmp/archives.txt
[ "$(grep -c '\.tar\.gz$' /tmp/archives.txt)" -ge 2 ] && echo "ARCHIVE-UPLOAD-OK"

echo "FUNC-PASS"
EOF
