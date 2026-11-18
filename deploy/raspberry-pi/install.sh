#!/bin/bash
# Install the Pi's system side: Wi-Fi watchdog, persistent journal, Wi-Fi
# power saving off, and the daemon's user service. Run on the Pi as the user
# the daemon runs as, from this directory (make setup-pi does both).
#
# Safe to run again: a file identical to the repository is left alone, one that
# differs is replaced with a .bak copy beside it, and /etc/default/net-watchdog
# is local configuration, only created when missing.
set -eu
cd "$(dirname "$0")"

changed=()
# install_file MODE SOURCE DEST [user]
install_file() {
    local sudo=sudo
    [ "${4:-}" = user ] && sudo=
    if $sudo cmp -s "$2" "$3" 2>/dev/null; then
        return
    fi
    if $sudo test -e "$3"; then
        $sudo cp -p "$3" "$3.bak"
    fi
    $sudo install -D -m "$1" "$2" "$3"
    changed+=("$3")
}

install_file 755 net-watchdog /usr/local/sbin/net-watchdog
install_file 644 net-watchdog.service /etc/systemd/system/net-watchdog.service
install_file 644 net-watchdog.timer /etc/systemd/system/net-watchdog.timer
[ -e /etc/default/net-watchdog ] || install_file 644 net-watchdog.default /etc/default/net-watchdog
install_file 644 journald-persistent.conf /etc/systemd/journald.conf.d/50-persistent.conf
install_file 644 wifi-powersave-off.conf /etc/NetworkManager/conf.d/wifi-powersave-off.conf
install_file 644 vcontrold-go.service "$HOME/.config/systemd/user/vcontrold-go.service" user

sudo systemctl daemon-reload
sudo systemctl enable --quiet --now net-watchdog.timer
if [[ " ${changed[*]} " == *" /etc/systemd/journald.conf.d/"* ]]; then
    sudo systemctl restart systemd-journald
fi

# The serial port, and starting at boot with nobody logged in.
sudo usermod -aG dialout "$(id -un)"
sudo loginctl enable-linger "$(id -un)"
systemctl --user daemon-reload
systemctl --user enable --quiet vcontrold-go

if [ ${#changed[@]} -eq 0 ]; then
    echo "already up to date"
else
    printf 'installed %s\n' "${changed[@]}"
fi
[[ " ${changed[*]} " == *" /etc/NetworkManager/"* ]] &&
    echo "Wi-Fi power saving: applies from the next connection, or reboot"
[ -x "$HOME/vcontrold-go/vcontrold-go" ] ||
    echo "next: make deploy, then: systemctl --user start vcontrold-go"
grep -q '^TARGETS=""' /etc/default/net-watchdog &&
    echo "net-watchdog pings the gateway only; add the broker to TARGETS in /etc/default/net-watchdog"
exit 0
