#!/bin/sh
# nest post-remove script (deb/rpm). POSIX sh, runs as root.
set -e

if [ -d /run/systemd/system ]; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

exit 0
