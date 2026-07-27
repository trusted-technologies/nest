#!/usr/bin/env bash
# install.sh — one-liner installer for nest (https://github.com/trusted-technologies/nest).
#
# Usage:
#   curl -fsSL https://mirror.trust-me.host/nest/install.sh | sudo bash
#   curl -fsSL https://mirror.trust-me.host/nest/install.sh | sudo bash -s -- --uninstall
#
# Environment:
#   NEST_MIRROR_BASE   project base URL (default: https://mirror.trust-me.host/nest)
#   NEST_KEYS_BASE     signing-key base URL (default: https://mirror.trust-me.host/keys)
#
# Supported families: Debian/Ubuntu (apt), RHEL/Fedora/CentOS & co (dnf/yum),
# openSUSE/SLES (zypper). Sets up the package repository, then installs nest.
set -euo pipefail

BASE="${NEST_MIRROR_BASE:-https://mirror.trust-me.host/nest}"
KEYS_BASE="${NEST_KEYS_BASE:-https://mirror.trust-me.host/keys}"
KEY_ASC="$KEYS_BASE/trusted-technologies.asc"
KEY_GPG="$KEYS_BASE/trusted-technologies.gpg"

log() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" -eq 0 ] || die "must run as root — try: curl -fsSL $BASE/install.sh | sudo bash"

# --- detect the distro family via os-release ----------------------------------
[ -r /etc/os-release ] || die "cannot detect the distribution (/etc/os-release missing)"
# shellcheck disable=SC1091
. /etc/os-release
ID="${ID:-}"
ID_LIKE=" ${ID_LIKE:-} "

family=""
case "$ID" in
    debian|ubuntu|raspbian)                       family=debian ;;
    centos|almalinux|rocky|ol|rhel|amzn|fedora)   family=rhel ;;
    opensuse-leap|opensuse-tumbleweed|opensuse|sles|sled) family=suse ;;
    *)
        case "$ID_LIKE" in
            *" debian "*|*" ubuntu "*)            family=debian ;;
            *" rhel "*|*" fedora "*|*" centos "*) family=rhel ;;
            *" suse "*|*" opensuse "*)            family=suse ;;
        esac
        ;;
esac

# --- uninstall: remove repository configuration -------------------------------
if [ "${1:-}" = "--uninstall" ]; then
    case "$family" in
        debian)
            rm -f /etc/apt/sources.list.d/nest.list \
                  /usr/share/keyrings/nest-archive-keyring.gpg
            apt-get update || true
            ;;
        rhel)
            rm -f /etc/yum.repos.d/nest.repo
            ;;
        suse)
            zypper --non-interactive removerepo nest 2>/dev/null \
                || rm -f /etc/zypp/repos.d/nest.repo
            ;;
        *)
            die "unknown distribution; remove the nest repository files manually"
            ;;
    esac
    log "nest repository removed. To remove the package itself:"
    log "  Debian/Ubuntu:  apt-get remove nest"
    log "  Fedora/RHEL:    dnf remove nest    (or: yum remove nest)"
    log "  openSUSE/SLES:  zypper remove nest"
    exit 0
fi

# --- install -------------------------------------------------------------------
case "$family" in
    debian)
        # curl fetches the key; ca-certificates only for https mirrors.
        need=""
        command -v curl >/dev/null 2>&1 || need="$need curl"
        case "$BASE" in
            https://*)
                [ -e /etc/ssl/certs/ca-certificates.crt ] || need="$need ca-certificates" ;;
        esac
        if [ -n "$need" ]; then
            apt-get update
            # shellcheck disable=SC2086
            DEBIAN_FRONTEND=noninteractive apt-get install -y $need
        fi

        install -d -m 0755 /usr/share/keyrings
        if command -v gpg >/dev/null 2>&1; then
            curl -fsSL "$KEY_ASC" \
                | gpg --dearmor > /usr/share/keyrings/nest-archive-keyring.gpg.tmp
        else
            # No gpg on the system: fetch the pre-dearmored keyring.
            curl -fsSL "$KEY_GPG" \
                > /usr/share/keyrings/nest-archive-keyring.gpg.tmp
        fi
        mv /usr/share/keyrings/nest-archive-keyring.gpg.tmp \
           /usr/share/keyrings/nest-archive-keyring.gpg
        chmod 0644 /usr/share/keyrings/nest-archive-keyring.gpg

        echo "deb [signed-by=/usr/share/keyrings/nest-archive-keyring.gpg] $BASE/apt stable main" \
            > /etc/apt/sources.list.d/nest.list
        # Refresh only the nest repo — a broken third-party source elsewhere
        # on the system must not abort the installation.
        apt-get update \
            -o Dir::Etc::SourceList=/etc/apt/sources.list.d/nest.list \
            -o Dir::Etc::SourceParts="-" \
            -o APT::Get::List-Cleanup="0"
        DEBIAN_FRONTEND=noninteractive apt-get install -y nest
        ;;

    rhel)
        cat > /etc/yum.repos.d/nest.repo <<EOF
[nest]
name=nest repository
baseurl=$BASE/yum
enabled=1
gpgcheck=1
gpgkey=$KEY_ASC
EOF
        rpm --import "$KEY_ASC"
        if command -v dnf >/dev/null 2>&1; then
            dnf install -y nest
        else
            yum install -y nest
        fi
        ;;

    suse)
        # Import the key into the rpm keyring first: zypper verifies the
        # signed repomd.xml against it.
        rpm --import "$KEY_ASC"
        zypper --non-interactive ar -f -g "$BASE/yum" nest
        zypper --non-interactive --gpg-auto-import-keys refresh
        zypper --non-interactive install -y nest
        ;;

    *)
        cat >&2 <<EOF
error: unsupported distribution (ID=${ID:-unknown}).

Manual installation:
  1. Download a binary for your architecture (amd64 / arm64 / armhf):
       curl -fsSLO $BASE/bin/nest-linux-amd64
       install -m 0755 nest-linux-amd64 /usr/bin/nest
  2. Or fetch a package by hand:
       deb: $BASE/apt/pool/main/n/nest/    (dpkg -i)
       rpm: $BASE/yum/                     (rpm -i)
  3. Configure nest:  sudo nest setup
EOF
        exit 1
        ;;
esac

log ""
log "nest installed successfully."
log "Run: sudo nest setup"
