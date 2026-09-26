# 5 GHz Wi-Fi hotspot on Linux — field guide

A distro-agnostic, copy-pasteable guide to turning a Linux machine into a **5 GHz**
access point that shares its uplink (Ethernet, tether, or its own Wi-Fi).
Written from a working setup on an Intel Wireless 8265 (`iwlwifi`) and hardened
against the failure modes that make a "5 GHz" hotspot slow or invisible.

Everything below applies to any `nl80211` + `hostapd` capable card.
Commands are marked when they need root.

---

## 0. The four layers

A hotspot that "connects but is wrong" is almost always failing exactly one layer.
Check them in order:

| Layer | What must be true | Failure symptom |
|---|---|---|
| 1. Radio / regulatory | Radio not rfkill-blocked; the channel is allowed for AP use | AP never appears in a scan, or hostapd exits |
| 2. hostapd mode | `hw_mode=a` **and** HT/VHT enabled (`ieee80211n=1`, `ieee80211ac=1`, `wmm_enabled=1`) | 5 GHz shows on the phone, but speed caps at ~20–25 Mbit/s |
| 3. L2 plumbing | AP vif created and `up` **before** hostapd starts | `nl80211: Match already configured` |
| 4. L3 plumbing | Gateway IP, DHCP/DNS, `ip_forward`, NAT, firewall INPUT accept | Clients connect but get no IP / no internet |

Most guides only cover layer 4. Layers 1–2 are where 5 GHz actually breaks.

---

## 1. Packages

```sh
# Arch
sudo pacman -S hostapd dnsmasq iw

# Debian / Ubuntu
sudo apt install hostapd dnsmasq iw

# Fedora
sudo dnf install hostapd dnsmasq iw
```

Optional: `qrencode` (join QR codes), `nmcli` (NetworkManager).

---

## 2. Check what the hardware can do

```sh
iw dev                                                # find the Wi-Fi iface, e.g. wlp1s0
iw list | grep -A12 "Supported interface modes"       # need: "* AP"
iw list | grep -A6 "valid interface combinations"
iw phy phy0 info | grep -E "MHz \[(3[6-9]|4[0-8])\]"  # 5 GHz channels (UNII-1)
```

Interpretation:

- `* AP` in the interface modes → the driver can host an AP.
- `valid interface combinations` containing
  `#{ managed } <= 1, #{ AP, ... } <= 1, ... #channels <= 1`
  → **only one channel at a time.** If you keep a station (client) connection on
  the same radio, the AP must run on the *same channel and width*. This is true
  for most Intel cards. In that case a 5 GHz AP is only possible while the
  station is on 5 GHz — otherwise disconnect the station first (or share
  Ethernet and leave the Wi-Fi radio idle).
- No 5 GHz frequency lines → the card is 2.4 GHz only.

---

## 3. Unblock the radio

A soft-blocked radio still lists interfaces; you only find out when the AP
refuses to start.

```sh
rfkill list                 # look for "Soft blocked: yes"
sudo rfkill unblock wifi
nmcli radio wifi on         # when NetworkManager drives the radio
nmcli -t -f WIFI,WIFI-HW radio
```

---

## 4. Pick a legal channel

```sh
iw reg get
```

Read the **last block** (the phy's own table — that is what the kernel enforces).
Per channel, the flags decide usability:

| Flag on the frequency line | Meaning for an AP |
|---|---|
| *(none, e.g. `(22.0 dBm)`)* | Usable now |
| `no IR` | No initiating radiation → **cannot beacon** |
| `passive scanning` | Cannot initiate → unusable for AP |
| `radar detection` (DFS flag) | Usable, but hostapd must do a CAC (60–600 s) and the AP drops clients if radar is detected |
| `disabled` | Not permitted in this regdomain |

Prefer **UNII-1: channels 36 / 40 / 44 / 48** (5170–5250 MHz). No DFS, no CAC,
allowed almost everywhere.

```sh
# If your card respects a user-set regdomain (NOT "self-managed" phys):
sudo iw reg set IN          # or US, DE, ...
```

> Some cards (recent Intel) report `phy#0 (self-managed)` — `iw reg set` is
> ignored for them and the driver's own table is authoritative. For those, the
> only in-band lever is hostapd's `country_code=` + `ieee80211d=1`, and the
> channel must already be in the driver table without `no IR`.

---

## 5. Keep NetworkManager off the AP interface

NetworkManager will try to manage a new AP vif and fight hostapd for it.

`/etc/NetworkManager/conf.d/99-unmanaged-ap0.conf`:

```ini
[keyfile]
unmanaged-devices=interface-name:ap0
```

```sh
sudo nmcli general reload
```

---

## 6. Create the AP interface

```sh
sudo iw phy phy0 interface add ap0 type __ap
sudo ip link set ap0 up            # MUST be up before hostapd starts
```

The vif is not persistent; recreate it after every boot/reboot.
Remove with `sudo iw dev ap0 del`.

---

## 7. `hostapd.conf` — the part that decides your speed

`/etc/hostapd/hostapd-5g.conf`:

```ini
interface=ap0
driver=nl80211
ssid=MyHotspot
hw_mode=a                  # a = 5 GHz, g = 2.4 GHz
channel=36

# --- 802.11n/ac: ALL of these lines are required ---
ieee80211n=1               # HT. Without it ht_capab is silently ignored -> legacy 802.11a (54 Mbit/s max)
wmm_enabled=1              # WMM. HT/VHT require it; without it the AP falls back to legacy rates
ht_capab=[HT40+]           # ch36: HT40+ (see the +/- rule below)
ieee80211ac=1              # VHT. 5 GHz only — invalid together with hw_mode=g
vht_oper_chwidth=1         # 1 = 80 MHz (0 = 20/40, 2 = 160)
vht_oper_centr_freq_seg0_idx=42   # 80 MHz block centre index, see table

wpa=2
wpa_passphrase=at-least-8-chars
wpa_key_mgmt=WPA-PSK
rsn_pairwise=CCMP
ignore_broadcast_ssid=0
```

### Traps, ordered by how often they bite

1. **`ht_capab` without `ieee80211n=1`** → the AP beacons on 5 GHz exactly as
   asked, but only with legacy 802.11a rates (6–54 Mbit/s). Symptom: the phone
   shows a 5 GHz connection and speed caps at ~20–25 Mbit/s.
2. **Missing `wmm_enabled=1`** → WMM stays off (`WMM/WME: no` in
   `iw dev ap0 station dump`), and HT/VHT cannot operate without WMM — the AP
   falls back to legacy rates.
3. **`ieee80211ac=1` with `hw_mode=g`** → hostapd refuses: VHT is 5 GHz only.
4. **Wrong/missing VHT centre** → hostapd logs `Frequency not present (seg0)`, or
   the AP comes up narrower than intended. The centre must match the channel's
   80 MHz block:

   | Channels | 80 MHz centre | `vht_oper_centr_freq_seg0_idx` |
   |---|---|---|
   | 36–48 | 5210 MHz | 42 |
   | 52–64 | 5290 MHz | 58 |
   | 100–112 | 5530 MHz | 106 |
   | 116–128 | 5610 MHz | 122 |
   | 132–144 | 5690 MHz | 138 |
   | 149–161 | 5775 MHz | 155 |

   Formula: `idx = (centre_MHz - 5000) / 5`.
5. **Mirroring while a station connection is up** (`#channels <= 1`): the AP
   must use the **station's channel and width**. Station on 157/80 MHz →
   `channel=157`, `vht_oper_chwidth=1`, `vht_oper_centr_freq_seg0_idx=155`.
   A width mismatch makes `iwlwifi` silently refuse to beacon.
6. **HT40 direction**: `[HT40+]` puts the secondary channel *above* the primary,
   `[HT40-]` below. At ch36 use `[HT40+]`; at ch40 use `[HT40-]`; ch44/48 use
   `[HT40-]`. On 2.4 GHz 40 MHz is antisocial and frequently degrades to 20 MHz.
7. **No `wpa_passphrase` / under 8 characters** → hostapd exits; WPA2 needs
   8–63 printable ASCII characters.

---

## 8. Gateway address, DHCP and DNS

```sh
sudo ip addr replace 10.42.0.1/24 dev ap0

sudo dnsmasq --interface=ap0 --bind-interfaces --except-interface=lo \
  --no-resolv --server=1.1.1.1 --server=1.0.0.1 \
  --dhcp-range=10.42.0.10,10.42.0.200,255.255.255.0,12h
```

- On Debian/Ubuntu the `dnsmasq` package ships a systemd unit that already binds
  `:53`; either configure that instance (`interface=ap0` in `/etc/dnsmasq.d/`) or
  run the standalone command above with `--bind-interfaces` to avoid
  "address already in use".
- `--log-dhcp` is worth adding while bringing a setup up: it prints
  DISCOVER → OFFER → REQUEST → ACK per client.

---

## 9. Forwarding and NAT

```sh
sudo sysctl -w net.ipv4.ip_forward=1

sudo iptables -t nat -A POSTROUTING -s 10.42.0.0/24 ! -d 10.42.0.0/24 -j MASQUERADE
sudo iptables -I FORWARD -i ap0 -j ACCEPT
sudo iptables -I FORWARD -o ap0 -m state --state RELATED,ESTABLISHED -j ACCEPT
sudo iptables -I INPUT   -i ap0 -j ACCEPT   # DHCP (67) / DNS (53) arrive as INPUT
```

- The `INPUT` rule matters: with UFW/firewalld defaults (`DROP`), clients
  authenticate and then never get a lease.
- With UFW, `sudo ufw allow in on ap0` is the managed equivalent.
- Persist across reboots with `iptables-save` + `netfilter-persistent`, or
  translate to nftables.

---

## 10. Run it as a service

```sh
sudo systemd-run --unit=my-hotspot --collect hostapd /etc/hostapd/hostapd-5g.conf
journalctl -u my-hotspot -f
sudo systemctl stop my-hotspot
```

Do **not** use hostapd's `-B` daemon mode while setting this up: it forks and
hides the early failure that tells you which layer broke. Foreground under a
transient unit gives journald logging and a clean lifecycle.

---

## 11. Verification

```sh
iw dev ap0 info            # ground truth: channel, width, centre, txpower
iw dev ap0 station dump    # per-client negotiated rates
```

Expected on a healthy 80 MHz 5 GHz link:

```
        Interface ap0
                type AP
                channel 36 (5180 MHz), width: 80 MHz, center1: 5210 MHz
                txpower 22.00 dBm

Station aa:bb:cc:dd:ee:ff (on ap0)
        WMM/WME:        yes
        signal:         -42 [-44, -42] dBm
        tx bitrate:     780.0 MBit/s VHT-MCS 9 80MHz VHT-NSS 2
        rx bitrate:     390.0 MBit/s VHT-MCS 9 80MHz VHT-NSS 1
```

Red flags:

| Observation | Meaning |
|---|---|
| `WMM/WME: no` **and** `tx bitrate: 54.0 MBit/s` | Legacy rates → trap 1/2 in §7 |
| `width: 20 MHz` when you configured 80 | Wrong VHT centre, or the station pinned the width |
| `signal` worse than about −70 dBm | Client too far / antenna limited; MCS collapses |
| High `tx retries` relative to `tx packets` | Co-channel interference or a 40/80 MHz width that neighbours occupy |

Finish with a real throughput test from the client (phone speed test, or
`iperf3` between the AP machine and a client). A 5 GHz link that negotiates
VHT80 will not be the bottleneck for a typical ≤100 Mbit/s plan.

---

## 12. Symptom → cause

| Symptom | Cause |
|---|---|
| AP never shows up in scans | Channel flagged `no IR` / `passive scanning` (§4), or hw_mode/channel mismatch |
| 5 GHz on the phone, ~20–25 Mbit/s | Missing `ieee80211n=1` and/or `wmm_enabled=1` (§7) |
| `Frequency not present (seg0)` | Wrong `vht_oper_centr_freq_seg0_idx` for the channel/width |
| `Hardware does not support configured channel` | Channel/width not permitted by the phy, or must mirror the station |
| `nl80211: Match already configured` | `ap0` was not `up` before hostapd started |
| Client connects, never gets an IP | `INPUT` DROP on `ap0` (UFW/firewalld); check `--log-dhcp` |
| DHCP works, no internet | `net.ipv4.ip_forward=0`, or missing `MASQUERADE`/FORWARD rules |
| AP dies when Wi-Fi is rescanned | `ap0` not marked unmanaged in NetworkManager |
| Works, then clients drop together | DFS radar detection on a DFS channel — move to 36–48 |
| AP is "running" but no client can see or join it | hostapd survived while the phy dropped the vif's channel (radio reset, rfkill toggle, something else taking the phy). `iw dev ap0 info` shows **no `channel:` line**; restarting fixes it until it happens again — set up the watchdog in §13 |
| AP comes up on 2.4 GHz although no station is connected | A managed vif can report a channel left over from a scan, and mirroring it drags the AP onto that band. Gate mirroring on an actual association: `iw dev wlp1s0 link \| grep -q '^Connected'` before reading channel/width |
| 2.4 GHz-only client cannot see the AP | The AP is 5 GHz only — expected |
| Works on AC, not on battery | Power-saving / regulatory-power limits on some drivers; test on AC |

---

## 13. Keeping the AP honest (self-healing)

A hotspot left running for hours fails quietly in two ways, and both present as
"the AP is on" while nothing works.

### 13.1 hostapd alive, vif dead

`iw dev ap0 info` printing **no `channel:` line** means the phy dropped the AP's
channel context while hostapd kept running and kept reporting `AP-ENABLED`. The
systemd unit stays `active`, so any status built on unit state alone lies, and
the AP never comes back until someone restarts it by hand.

Make the state honest — the AP counts as up only when hostapd runs **and** the
vif holds a channel:

```sh
ap_up() {
  systemctl is-active --quiet my-hotspot &&
    iw dev ap0 info 2>/dev/null | grep -q 'channel'
}
```

Then add a watchdog plus a marker, so that a hotspot the user switched off
stays off:

```sh
# on a successful start: the AP is wanted
: >/run/my-hotspot/enabled
# on an explicit user stop: forget it
rm -f /run/my-hotspot/enabled

watchdog() {
  [ -e /run/my-hotspot/enabled ] || return 0   # user wants it off
  ap_up && return 0
  logger -t my-hotspot "wanted but not serving; restarting"
  stop_ap        # teardown must NOT clear the marker: a restart that fails
  start_ap       # (radio still rfkill'd) has to keep retrying
}
```

Drive it from a timer:

```ini
# /etc/systemd/system/my-hotspot-watchdog.timer
[Timer]
OnBootSec=45
OnUnitActiveSec=30

[Install]
WantedBy=timers.target
```

```sh
sudo systemctl enable --now my-hotspot-watchdog.timer
journalctl -u my-hotspot-watchdog -f
```

The subtle part is the marker: clear it in the *stop* path that a user action
calls, never in the shared teardown the watchdog uses. A first version that
clears it on every stop loses the intent the first time a restart fails, and
then the watchdog sits idle while the AP stays dead.

Prove the repair works without touching hardware:

```sh
nmcli radio wifi off      # the phy goes away, ap0 loses its channel
nmcli radio wifi on       # the watchdog should restore the AP within ~30 s
```

While the radio is off, `iw dev ap0 info` has no channel, the status reports
`off`, and hostapd is still `active` — exactly the silent state this section
exists for.

### 13.2 Log the band you picked

When the AP comes up on the wrong band, a record is worth having: the caller
(panel, TUI) usually swallows stderr, so write the decision to the journal.

```sh
logger -t my-hotspot "serving ap0: hw_mode=a channel=36 width=80 ht=[HT40+]"
```

`journalctl -t my-hotspot` then answers "why is it on 2.4 GHz this time?" — for
example because a stale station channel was mirrored (§12).

## 14. Teardown

```sh
sudo systemctl stop my-hotspot
sudo pkill -f "dnsmasq.*--interface=ap0"
sudo iw dev ap0 del
sudo iptables -D INPUT -i ap0 -j ACCEPT
sudo iptables -D FORWARD -i ap0 -j ACCEPT
sudo iptables -D FORWARD -o ap0 -m state --state RELATED,ESTABLISHED -j ACCEPT
sudo iptables -t nat -D POSTROUTING -s 10.42.0.0/24 ! -d 10.42.0.0/24 -j MASQUERADE
```

---

## 15. Alternatives

### NetworkManager one-liner

```sh
nmcli device wifi hotspot ifname wlp1s0 ssid MyHotspot password 'at-least-8' band a channel 36
nmcli connection down Hotspot        # stop
```

- Fast, no config files or NAT rules — NetworkManager sets up dnsmasq and
  connection sharing itself.
- `band` accepts `a` (5 GHz), `bg` (2.4 GHz), `6GHz`.
- Limitations: no control over VHT width, and no concurrent station+AP on
  single-channel cards. 6 GHz additionally requires WPA3/SAE (`ieee80211ax`).

### `create_ap` (wrapper used by this repo's TUI)

`/etc/create_ap.conf` — a working 5 GHz profile is in
[`examples/create_ap.conf`](../examples/create_ap.conf); the essentials:

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

- `IEEE80211N=1` is what turns on HT — without it you get legacy rates even at
  5 GHz. `COUNTRY=` also makes create_ap run `iw reg set` and emit
  `country_code=` + `ieee80211d=1`.
- `FREQ_BAND=5` is derived automatically when `CHANNEL > 14`, but set it
  explicitly for clarity.
- create_ap emits **no** `vht_oper_chwidth` / `vht_oper_centr_freq_seg0_idx`
  keys, so it cannot pin an 80 MHz block. That is fine for typical home
  uplinks; for 80 MHz use the manual hostapd route in §7 (or append those two
  keys to the generated `hostapd.conf`).
- Verify with `create_ap --list-running` and `iw dev ap0 station dump`.

---

## 16. Reference numbers

Measured on: Intel Wireless 8265 (`iwlwifi`), 5 GHz channel 36, VHT80, phone as
client one metre away, 50 Mbit/s uplink:

| Configuration | Client `tx bitrate` | Result |
|---|---|---|
| 5 GHz, legacy (no `ieee80211n`) | 54.0 Mbit/s | ~20–25 Mbit/s real — radio-bound |
| 5 GHz, `ieee80211n=1` + `wmm_enabled=1` + VHT80 | 780.0 Mbit/s (MCS 9, NSS 2) | uplink-bound: full plan speed |

The lesson: **"5 GHz" alone is not a speed class.** A 5 GHz AP running legacy
rates is slower than a well-configured 2.4 GHz one.
