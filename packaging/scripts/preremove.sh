#!/bin/sh
# nest pre-remove script (deb/rpm). POSIX sh, runs as root.
#
# $1 convention differs between package managers:
#   deb: "remove" | "upgrade" | "deconfigure" | "failed-upgrade"
#   rpm: 0 = final removal (erase), 1 = upgrade
# We only stop/disable the timer on an actual removal, never on upgrade.
set -e

case "${1:-}" in
    remove|0)
        if [ -d /run/systemd/system ]; then
            systemctl disable --now nest.timer >/dev/null 2>&1 || true
        fi
        ;;
esac

exit 0
