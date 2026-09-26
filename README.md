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

A known-good 5 GHz profile is in [`examples/create_ap.conf`](examples/create_ap.conf):

```ini
WIFI_IFACE=wlp1s0
INTERNET_IFACE=enp3s0f3u2
SSID=MyHotspot
PASSPHRASE=change-me-please
CHANNEL=36
FREQ_BAND=5
COUNTRY=IN
IEEE80211N=1
IEEE80211AC=1
HT_CAPAB=[HT40+]
```

`IEEE80211N=1` is the one that matters most: without it `HT_CAPAB` is ignored
and the AP runs legacy 802.11a rates (54 Mbit/s PHY, ~20–25 Mbit/s real) even
though it beacons on 5 GHz.

Verify from the AP machine once running:

```bash
iw dev ap0 info            # channel 36 (5180 MHz), width: 40/80 MHz
iw dev ap0 station dump    # per client: WMM/WME: yes, VHT-MCS rates (not 54.0 MBit/s)
```

## 5 GHz setup guide

[`docs/5ghz-hotspot-guide.md`](docs/5ghz-hotspot-guide.md) is a distro-agnostic,
step-by-step guide for building a 5 GHz hotspot from scratch on a fresh Linux
install — manual `hostapd` + `dnsmasq` + NAT, the `nmcli` one-liner, and this
repo's `create_ap` route, including:

- hardware capability checks (`iw list`: AP mode, interface combinations),
- regulatory-domain/channel selection (UNII-1 36–48, DFS and `no IR` pitfalls),
- the `hostapd.conf` lines that decide throughput
  (`ieee80211n` / `wmm_enabled` / `ieee80211ac` + VHT centre index table),
- DHCP/DNS/NAT/firewall plumbing,
- verification with expected `iw dev ap0 station dump` output,
- a self-healing watchdog for an AP that silently loses its radio while
  hostapd keeps running,
- a symptom → cause table for everything above.

