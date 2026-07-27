#!/bin/sh
# nest post-install script (deb/rpm). POSIX sh, runs as root.
set -e

# State, cache, config and SSH-key directories.
mkdir -p /etc/nest /var/lib/nest/bin /var/cache/nest /etc/nest/ssh
chmod 700 /etc/nest/ssh

# Register the new unit files with systemd (if systemd is running).
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

# Fresh install? deb calls: postinst configure <old-version (empty on install)>
# rpm calls: postinstall 1 (install) / 2 (upgrade)
fresh=0
if [ "${1:-}" = "configure" ] && [ -z "${2:-}" ]; then
    fresh=1
elif [ "${1:-}" = "1" ]; then
    fresh=1
fi

if [ "$fresh" = "1" ]; then
    echo ""
    echo "nest installed. Next steps:"
    echo "  1. Configure remotes and jobs:  sudo nest setup"
    echo "  2. Enable scheduled backups:    sudo systemctl enable --now nest.timer"
    echo ""
fi

exit 0
