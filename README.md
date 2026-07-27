# nest

Simple server backups: folders and databases to S3, SFTP or any rclone remote — incremental, encrypted, scheduled, with an interactive setup TUI.

nest is a single static Go binary. Point it at your data, pick a destination, and a systemd timer keeps your backups running. Restores are one command away.

## Features

- 🪺 **Interactive TUI** — `sudo nest setup` walks you through remotes, jobs and schedules; no hand-written YAML required (but supported).
- **Multiple destinations** — any S3-compatible storage, SFTP (SSH key auth), or any remote configured in `rclone config`.
- **Two backends per job** — restic (incremental, deduplicated, encrypted) or plain `.tar.gz` archives.
- **Ready-made job presets** — Pterodactyl Volumes, Pterodactyl Backups, Folder, MySQL/MariaDB, PostgreSQL.
- **Flexible scheduling** — daily "HH:MM" or full 5-field cron expressions, driven by a systemd timer.
- **Retention policies** — keep last/daily/weekly/monthly snapshots per job.
- **Telegram notifications** — success/failure reports straight to your chat.
- **Self-updating** — `sudo nest self-update` pulls the latest binary from the mirror.
- **Wide distro support** — deb and rpm packages plus apt/dnf/zypper repositories; x86_64, aarch64 and armhf.

## Supported distributions

| Distribution | Versions | Architectures |
|---|---|---|
| AlmaLinux | 8–10 | x86_64, aarch64 |
| Amazon Linux | 2 & 2023 | x86_64, aarch64 |
| CentOS | 7–10 | x86_64, aarch64 |
| Debian | 10–13 | x86_64, aarch64, armhf |
| openSUSE Leap | 15–16 | x86_64, aarch64 |
| Oracle Linux | 7–10 | x86_64, aarch64 |
| Raspberry Pi OS | 10–13 | armhf, aarch64 |
| RHEL | 7–10 | x86_64, aarch64 |
| Rocky Linux | 8–10 | x86_64, aarch64 |
| SLES | 12 SP4+ / 15 SP4+ / 16 | x86_64, aarch64 |
| Ubuntu | 16.04 / 18.04 / 20.04 / 22.04 / 24.04 / 26.04 | x86_64, aarch64, armhf |

Other distributions can use the standalone binaries from the mirror (see *Manual installation* below).

## Quick start

```bash
curl -fsSL https://mirror.trust-me.host/nest/install.sh | sudo bash
sudo nest setup
```

The installer detects your distribution, adds the nest package repository (apt, dnf/yum or zypper) and installs the package. `nest setup` then opens the interactive TUI to configure remotes and backup jobs.

To enable scheduled backups after setup:

```bash
sudo systemctl enable --now nest.timer
```

## Manual repository setup

### Debian / Ubuntu (apt)

```bash
curl -fsSL https://mirror.trust-me.host/keys/trusted-technologies.asc \
  | sudo gpg --dearmor -o /usr/share/keyrings/nest-archive-keyring.gpg
echo "deb [signed-by=/usr/share/keyrings/nest-archive-keyring.gpg] https://mirror.trust-me.host/nest/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/nest.list
sudo apt-get update
sudo apt-get install nest
```

### Fedora / RHEL / CentOS / Alma / Rocky / Oracle / Amazon (dnf/yum)

```bash
sudo tee /etc/yum.repos.d/nest.repo <<'EOF'
[nest]
name=nest repository
baseurl=https://mirror.trust-me.host/nest/yum
enabled=1
gpgcheck=1
gpgkey=https://mirror.trust-me.host/keys/trusted-technologies.asc
EOF
sudo rpm --import https://mirror.trust-me.host/keys/trusted-technologies.asc
sudo dnf install nest        # or: yum install nest
```

### openSUSE / SLES (zypper)

```bash
sudo zypper ar -f -g https://mirror.trust-me.host/nest/yum nest
sudo zypper --gpg-auto-import-keys refresh
sudo zypper install nest
```

### Manual installation (standalone binary)

```bash
# amd64 — use arm64 or armhf on ARM machines
curl -fsSLO https://mirror.trust-me.host/nest/bin/nest-linux-amd64
sudo install -m 0755 nest-linux-amd64 /usr/bin/nest
sudo nest setup
```

Checksums for every binary are published at `https://mirror.trust-me.host/nest/bin/sha256sums.txt`.

## Usage

### Interactive setup

`sudo nest setup` (or just `sudo nest`) opens the TUI:

1. **Remotes** — add a destination:
   - **s3**: endpoint, region, bucket, access/secret key (works with AWS S3, Wasabi, MinIO, Cloudflare R2, ...). Optional `path_prefix` separates multiple servers inside one bucket.
   - **sftp**: host, port, user. Key auth only — nest generates `/etc/nest/ssh/id_ed25519` if needed; authorize the public key on the backup server.
   - **rclone**: pick any remote from your `rclone config` (e.g. `gdrive:backups`).
2. **Jobs** — start from a preset (Pterodactyl Volumes `/var/lib/pterodactyl/volumes`, Pterodactyl Backups `/var/lib/pterodactyl/backups`, Folder, MySQL/MariaDB, PostgreSQL) and adjust:
   - source directory or database credentials,
   - target remote,
   - backend: **incremental** (restic) or plain `.tar.gz` archives,
   - schedule, retention (last/daily/weekly/monthly), exclude patterns.
3. **Notifications** — optionally enable Telegram.

The configuration is stored in `/etc/nest/config.yml` (mode 0600).

### Command line

| Command | Description |
|---|---|
| `nest`, `nest setup` | Open the interactive setup TUI |
| `nest backup [--job N] [--scheduled] [--window-minutes 5]` | Run backups: one job, everything, or only jobs due now (`--scheduled`) |
| `nest restore --job N [--snapshot latest] [--target DIR] [--path P]` | Restore a snapshot or archive |
| `nest jobs` | List configured jobs |
| `nest remotes` | List configured remotes |
| `nest snapshots --job N` | List snapshots of a job |
| `nest repair --job N [--full]` | Check/repair a restic repository |
| `nest version` | Print the version |
| `nest self-update` | Update the nest binary from the mirror |

Global flags: `-c/--config` (alternate config path), `-v/--verbose`. See `nest <command> --help` for details.

### Scheduling

The systemd timer runs `nest backup --scheduled` every 5 minutes; a job runs when its schedule falls inside the 5-minute window. Schedule formats:

- `"04:00"` — daily at 04:00,
- `"*/30 * * * *"` — any 5-field cron expression,
- empty — run on every timer tick (every 5 minutes).

Run an ad-hoc backup any time with `nest backup` (all enabled jobs) or `nest backup --job mysql`.

### Restore examples

```bash
# list snapshots of a job
nest snapshots --job pterodactyl-volumes

# restore the latest snapshot to a directory
nest restore --job pterodactyl-volumes --snapshot latest --target /srv/restore

# restore a single path out of a snapshot
nest restore --job mysql --snapshot latest --path dumps/mydb.sql --target /tmp/mysql-restore
```

### Telegram notifications

1. Create a bot with [@BotFather](https://t.me/BotFather) and copy the token.
2. Get your chat id (e.g. message the bot, then open `https://api.telegram.org/bot<token>/getUpdates`).
3. Enter both in `nest setup` → Notifications, or set `notifications.telegram` in the config.

### Self-update

```bash
sudo nest self-update
```

Downloads the latest binary for your architecture from the mirror and replaces `/usr/bin/nest`.

### Logs

Backups run through systemd, so logs live in the journal:

```bash
journalctl -u nest.service          # backup runs
journalctl -u nest.timer            # timer triggers
systemctl list-timers nest.timer    # next scheduled run
```

## Configuration reference

`/etc/nest/config.yml` (mode 0600) is plain YAML. It is normally managed by `nest setup`; the annotated example below (also shipped as `/etc/nest/config.yml.example`) shows every field:

```yaml
version: 1                          # config format version

# Default restic repository password (per-job override: restic_password).
# May also come from the NEST_RESTIC_PASSWORD environment variable.
encryption_password: "change-me-long-random-string"

remotes:
  - name: my-s3                     # referenced by jobs[].remote
    type: s3                        # s3 | sftp | rclone
    endpoint: s3.eu-central-1.amazonaws.com
    region: eu-central-1
    bucket: my-backup-bucket
    access_key: AKIAEXAMPLE
    secret_key: "super-secret"
    path_prefix: web-1              # optional path prefix for all repos

  - name: backup-server
    type: sftp
    host: 192.0.2.10
    port: 22
    user: backup
    key_file: /etc/nest/ssh/id_ed25519

  # - name: gdrive
  #   type: rclone
  #   rclone_remote: gdrive:backups # a remote from `rclone config`

jobs:
  - name: pterodactyl-volumes
    type: folder                    # folder | mysql | postgresql
    preset: pterodactyl-volumes     # informational: preset the job came from
    source: /var/lib/pterodactyl/volumes
    remote: my-s3
    incremental: true               # true = restic, false = .tar.gz archives
    schedule: "04:00"               # "HH:MM" or 5-field cron; empty = every tick
    retention:
      last: 0
      daily: 3
      weekly: 1
      monthly: 0
    exclude:
      - "*/cache/**"
    enabled: true

  - name: mysql
    type: mysql
    remote: backup-server
    incremental: true
    schedule: "03:00"
    retention:
      daily: 7
    db_host: localhost
    db_port: 3306
    db_user: root
    db_password: "secret"
    databases: []                   # empty = all databases
    enabled: true

notifications:
  telegram:
    enabled: false
    bot_token: "123456:ABC-your-bot-token"
    chat_id: "123456789"
    thread_id: ""                   # optional forum topic id
```

Paths used by nest:

| Path | Purpose |
|---|---|
| `/usr/bin/nest` | the binary |
| `/etc/nest/config.yml` | configuration (mode 0600) |
| `/etc/nest/ssh/` | SSH keypair for SFTP remotes (mode 0700) |
| `/var/lib/nest/` | state: restic/rclone helper binaries downloaded on demand |
| `/var/cache/nest/` | restic cache |

## How it works

- **restic backend** (`incremental: true`) — snapshots are deduplicated, compressed and encrypted inside a restic repository on the remote. Supports per-snapshot restore, partial (`--path`) restore, retention policies and `nest repair`.
- **archive backend** (`incremental: false`) — every run uploads a full `.tar.gz` archive. Simple and portable, but no deduplication or partial restore.
- **Helper binaries** — nest does not bundle restic/rclone. On first use it downloads the right build for your architecture from the mirror (`https://mirror.trust-me.host/nest/bin/`, checksums in `sha256sums.txt`) into `/var/lib/nest/`. No internet access to the mirror? Install the `restic`/`rclone` distro packages instead (recommended by the deb package).
- **Scheduling** — `nest.timer` triggers `nest.service` every 5 minutes, which runs `nest backup --scheduled`; each job whose schedule matches the current 5-minute window is executed.

## Building from source

Requirements: Go 1.25+ (a repo-local toolchain under `.tools/go` is used automatically when `go` is not in `PATH`).

```bash
make build      # static binaries for amd64/arm64/armhf -> dist/bin/
make test       # go test ./...
make vet        # go vet ./...
make package    # .deb/.rpm via nfpm           -> dist/packages/
make tools      # download restic/rclone        -> dist/tools/bin/
make repo       # signed apt + yum repositories -> dist/repo/  (needs NEST_GPG_KEY)
make publish    # sync everything to the mirror (needs R2 credentials)
make clean
```

The version is injected via `-ldflags -X github.com/trusted-technologies/nest/internal/buildinfo.Version=...` and resolved as follows: an exact git tag on HEAD wins (semver releases); in CI every other run gets an auto-incrementing `1.0.<run-number>` (each push to `main` publishes a newer package); local untagged builds fall back to `dev`.

## Releasing / publishing the mirror

Releases are fully automated: pushing a `v*` tag runs [`.github/workflows/release.yml`](.github/workflows/release.yml), which builds, signs and publishes everything, then attaches the packages to a GitHub release.

The public mirror (Cloudflare R2, `https://mirror.trust-me.host`) is shared between projects; nest lives under the `/nest` prefix and one GPG key signs all projects under `/keys`:

```
/nest/install.sh              one-liner installer
/nest/VERSION                 latest released version
/nest/bin/                    nest/restic/rclone binaries + sha256sums.txt
/nest/apt/                    Debian/Ubuntu repository (signed: InRelease)
/nest/yum/                    RPM repository (signed: repomd.xml.asc)
/keys/trusted-technologies.asc   repository GPG public key (ascii-armored)
/keys/trusted-technologies.gpg   same key, pre-dearmored for minimal systems
```

To publish by hand:

```bash
# one-time: generate the repo signing key
./scripts/gen-repo-key.sh

export NEST_GPG_KEY=<fingerprint>     # key used to sign the repos
export R2_ENDPOINT=https://<account-id>.r2.cloudflarestorage.com
export R2_BUCKET=<bucket>
export AWS_ACCESS_KEY_ID=...
export AWS_SECRET_ACCESS_KEY=...

make build package tools repo publish
```

CI secrets: `GPG_PRIVATE_KEY` (armored private key), `R2_ENDPOINT`, `R2_BUCKET`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`.

## Security notes

- `/etc/nest/config.yml` contains credentials and the restic encryption password; nest always writes it with mode **0600**. Back it up securely — without `encryption_password` your restic snapshots are unrecoverable.
- SFTP remotes use **SSH key authentication only**; nest generates a dedicated keypair in `/etc/nest/ssh/` (mode 0700).
- The apt and yum repositories are GPG-signed and clients verify signatures (`signed-by` / `gpgcheck=1`); all traffic to the mirror is over HTTPS.
- The systemd service runs backups with low priority (`Nice=10`, idle-IO class) to avoid interfering with production workloads.

## Contributing

Issues and pull requests are welcome at [github.com/trusted-technologies/nest](https://github.com/trusted-technologies/nest). Please run `make test` and `make vet` before submitting.

## License

MIT — see [LICENSE](LICENSE).
