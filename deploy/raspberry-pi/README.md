# Running on a Raspberry Pi

The daemon runs on a Raspberry Pi Zero W fixed next to the boiler, in a
basement where the Wi-Fi is weak. This directory holds everything installed on
it besides the binary and its configuration, with the reason for each piece.
Most of those reasons are outages.

Nothing here carries an address or a credential. Those live in the
`CHANGEME`s of `cloud-init/`, in `/etc/default/net-watchdog` on the Pi, and in
the gitignored `.env` and `config-openv.yaml`.

## Hardware

| Part | Detail |
|---|---|
| Board | Raspberry Pi Zero W Rev 1.1: ARMv6, one core, 512 MB. Build with `TARGET_GOARCH=arm TARGET_GOARM=6`; a Zero 2 W would take `arm64`. |
| Optolink head | USB optical head with a CH340 USB-serial chip (`1a86:7523`, kernel driver `ch341`), on the Optolink port of the Vitotronic front panel. The daemon sets 4800 baud, 8E2. |
| USB | The Zero W has one data port, the micro-USB marked USB; the other one only powers the board. The head goes through an OTG adapter or a small hub. |
| System | Raspberry Pi OS Lite, 32-bit (armhf), on an SD card. |
| Network | Wi-Fi only, around −80 dBm at the boiler. |

The head shows up as `/dev/serial/by-id/usb-1a86_USB_Serial-if00-port0`, the
`device` to put in `config-openv.yaml`. The CH340 has no serial number, so that
name only stays unique while it is the sole adapter of its kind plugged in.

The daemon uses about 10 MB of memory and under 1 % of the CPU. The board is
more than enough.

## The files

| File | Installed as | Role |
|---|---|---|
| `cloud-init/user-data`, `cloud-init/network-config` | the card's boot partition | first boot: user, SSH key, Wi-Fi |
| `install.sh` | | installs everything below |
| `vcontrold-go.service` | `~/.config/systemd/user/` | the daemon, as a user service |
| `net-watchdog` | `/usr/local/sbin/` | repairs the Wi-Fi, reboots as a last resort |
| `net-watchdog.service`, `net-watchdog.timer` | `/etc/systemd/system/` | run it every minute |
| `net-watchdog.default` | `/etc/default/net-watchdog` | its settings: local, created once, never overwritten |
| `journald-persistent.conf` | `/etc/systemd/journald.conf.d/50-persistent.conf` | keeps the journal across reboots |
| `wifi-powersave-off.conf` | `/etc/NetworkManager/conf.d/` | Wi-Fi power saving off |

## Installing

1. Write Raspberry Pi OS Lite (32-bit) to the card. Fill in the `CHANGEME`s in
   `cloud-init/` and copy both files over the ones on the boot partition.
2. Boot the Pi and check that `ssh` works.
3. On the development machine, in `.env`:

   ```bash
   TARGET_HOST=user@pi-host
   TARGET_DIR=/home/user/vcontrold-go   # the service expects ~/vcontrold-go
   TARGET_GOARCH=arm
   TARGET_GOARM=6
   ```

4. Then:

   ```bash
   make setup-pi                                 # this directory, through install.sh
   ssh user@pi-host sudoedit /etc/default/net-watchdog   # TARGETS: gateway and broker
   make deps deploy                              # binary, configuration, XML
   ssh user@pi-host systemctl --user start vcontrold-go
   ```

`make setup-pi` can be run again at any time. It replaces only the files that
differ from the repository, keeps the old one as `.bak`, and never touches
`/etc/default/net-watchdog`.

## Why each piece is there

### A user service that starts before the network

The daemon runs as a systemd user service. It needs no root, only the `dialout`
group for the serial port. Lingering (`loginctl enable-linger`) starts it at
boot with nobody logged in. The user manager keeps the groups it started with,
so after joining `dialout`, reboot or the port stays "Permission denied".

A user unit cannot wait for the network. `network-online.target` is a system
unit that the user manager does not see, and `After=network-online.target` in a
user unit is silently ignored. So the daemon starts before the Wi-Fi is up and
has to cope: it retries the broker every 30 seconds from start-up, and writes
what it cannot deliver to the ledger.

Until November 2026 it gave up after one failed connection instead. Each
reboot then left it reading the boiler and publishing nothing, for more than two days,
until someone restarted it by hand.

### The network watchdog

The Zero W sometimes loses the access point and does not reconnect by itself.
`net-watchdog` pings the targets every minute. As failed minutes add up, it
escalates:

| After | Action |
|---|---|
| 3 min | disconnects and reconnects the interface through NetworkManager |
| 6 min | reloads the `brcmfmac` driver, because the chip can wedge |
| 10 min | reboots, at most once every six hours; in between, reconnects every 10 min |

The six-hour limit comes from an outage where it rebooted every hour, ten times
in a row, and none of it helped: the link came back on its own. Every reboot
also cuts the boiler readings. When the fault is beyond the Pi, the cheap
reconnect is the better thing to repeat.

Each step logs a snapshot under the `net-watchdog` tag: the interface's address,
NetworkManager's state, the signal from `/proc/net/wireless` and the end of
`dmesg`.

### A persistent journal

Raspberry Pi OS keeps the journal in RAM (`40-rpi-volatile-storage.conf`). Each
reboot erased the log of what led to it, and those were exactly the reboots
that needed explaining. `journald-persistent.conf` keeps the journal on the
card, capped at 64 MB.

At the default `info` level the daemon no longer logs individual readings.
They once filled those 64 MB in about thirty hours, so the journal now holds
incidents rather than readings.

### Wi-Fi power saving off

On the Zero W, Wi-Fi power saving drops long-lived idle connections, and the
MQTT connection is one of them.

### The ledger

Readings that cannot be delivered go to `~/vcontrold-go/ledger/` and can be
imported into Home Assistant's statistics afterwards. See "When the broker or
Home Assistant is unreachable" in the [main README](../../README.md).

## Checking on it

```bash
systemctl --user status vcontrold-go
journalctl --user -u vcontrold-go --since -1d   # connections, losses, ledger, errors
journalctl -t net-watchdog --since -7d          # every repair and every reboot
curl -s localhost:8080/api/mqtt/status          # connected, haOnline, ledger
ls ~/vcontrold-go/ledger/                       # a file here: readings to backfill
awk '/wlan0/ {print $4}' /proc/net/wireless     # signal level, dBm
```

## What this does not fix

All of the above limits the damage of a lost link. Only a better link removes
it: an access point closer to the boiler, or Ethernet through a USB adapter on
the hub.
