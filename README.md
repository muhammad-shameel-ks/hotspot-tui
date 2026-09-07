# hotspot-tui

Terminal UI for hosting a 5 GHz WiFi hotspot on Linux via `create_ap` + `hostapd`.
Built for Intel cards (tested on 8265) where the default channel needs pinning to a usable 5 GHz channel.

## Install

You need Go 1.24+, `create_ap`, `hostapd`, `iw`, and `nmcli`.

```bash
go build -o hotspot-tui .
cp hotspot-tui ~/.local/bin/
```

## Use

```bash
hotspot-tui
```

Keys: `s` start, `x` stop, `e` edit SSID/password, `v` show WiFi QR, `r` refresh, `esc` detach (AP keeps running).

Headless flags: `--status`, `--start`, `--stop`, `--qr`.

## Config

Settings live in `/etc/create_ap.conf` (channel, country, HT/VHT flags).
The TUI reads and updates SSID/PASSPHRASE there via sudo.
