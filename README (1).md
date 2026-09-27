<div align="center">

# Hashem Panel 🇮🇷 ↔ 🌍

**GRE Layer-3 tunnel + encrypted TLS FRP reverse relay — with a premium web panel.**

[![Latest Release](https://img.shields.io/github/release/pdnczone/hashem-panel?display_name=tag)](https://github.com/pdnczone/hashem-panel/releases/latest)
[![Build Panel](https://github.com/pdnczone/hashem-panel/actions/workflows/build-panel.yml/badge.svg)](https://github.com/pdnczone/hashem-panel/actions/workflows/build-panel.yml)
[![Platform](https://img.shields.io/badge/platform-linux%20amd64%20%7C%20arm64-blue)](https://github.com/pdnczone/hashem-panel)
[![Website](https://img.shields.io/badge/website-pdnczone.ir-38bdf8)](https://pdnczone.ir)
[![Telegram](https://img.shields.io/badge/telegram-@pdnczone-229ED9)](https://t.me/pdnczone)
[![YouTube](https://img.shields.io/badge/youtube-@pdnczone-FF0000)](https://youtube.com/@pdnczone)

[🌐 Website](https://pdnczone.ir) · [✈️ Telegram](https://t.me/pdnczone) · [▶️ YouTube](https://youtube.com/@pdnczone)

_Iran's IP stays behind the tunnel — foreign-server ports become reachable through Iran's public IP._

> **خلاصه فارسی:** تونل لایه ۳ GRE + ریورس رمزنگاری‌شده FRP با پنل وب حرفه‌ای. با یک خط نصب کن، گزینه `1` روی ایران و گزینه `2` روی سرور خارج. پنل وب خودکار نصب می‌شه و آخر نصب لینک + یوزر + پسورد رو نشون می‌ده. بعدش همه‌چیز هم از ترمینال (`hashem`) هم از مرورگر قابل مدیریته.

</div>

---

## ✨ Features

- 🚀 **One-line install** — prebuilt panel binary from GitHub releases, no Go needed on servers
- 🖥️ **Premium web panel** — 7 tabs: Dashboard · Tunnel · Setup · Logs · Update · Settings · Terminal
- ⌨️ **`hashem` CLI** — one command in SSH opens the full tunnel menu (options 0–18)
- 🌐 **Multi-peer** — up to 5 foreign servers on one Iran, each with its own token + card in the Tunnel tab
- 📦 **Setup bundle** — one `hsh1_...` string carries IP + control port + GRE pair + token + ports; paste it on Foreign and everything auto-fills (legacy 32-char tokens still work)
- 🗑️ **Per-peer remove that works** — inline two-step confirm (arm 5s + Undo) inside each card; falls back to direct removal on servers with a stale installer
- 🃏 **Dashboard-style cards** — Main tunnel card (gold accent) + per-peer cards (Foreign/IP, GRE, FRP & traffic) in Tunnel, peer summary rows in Dashboard that jump to the full card
- 🔒 **Panel HTTPS** — Let's Encrypt from Settings or terminal, HTTP + HTTPS side by side, auto-renew
- 💻 **Interactive terminal** — real root shell in the browser (xterm.js + WebSocket + PTY), feature-flagged, audit-logged
- 🧾 **Useful logs + error codes** — every failure maps to a stable `E-XXXX` code with hints, surfaced in Logs → Panel errors
- 📊 **Premium charts** — total-traffic chart with 24H / 7D / 30D ranges
- ⚡ **Optimize tunnel** — BBR + buffers + MTU/MSS tuning with backup & restore
- 🔄 **Safe updates** — script + panel update with config backup and rollback

---

## 🏗️ Architecture

```
Users ──► IRAN (public ports here) ══ GRE + FRP ══► FOREIGN (service runs here)
          frps listens :7000/:443…              frpc dials via 10.10.10.2
          GRE 10.10.10.2/30                     GRE 10.10.10.1/30
```

| Part | Iran server | Foreign server |
|------|-------------|----------------|
| GRE | `10.10.10.2/30` (`gre-tunnel`, systemd) | `10.10.10.1/30` (`gre-tunnel`, systemd) |
| FRP | `frps` (server, TLS) | `frpc` (client, connects to `10.10.10.2` **inside** the tunnel) |
| Peers 2–5 | `frps-N` + `gre-tN` per peer | own `frpc` per peer |
| Services | `systemd`, auto-start on boot | `systemd`, auto-start on boot |
| Web panel | `gre-panel` on `:7777/<secret>` (+ `:7443` with cert) | same, its own side only |

- Auto arch detect (`amd64` / `arm64`), FRP download, `ip_forward` + TCPMSS clamp
- Every port = `tcp` + `udp` proxy with the same number on Iran
- Each panel manages **only its own side** (no remote control)

---

## ⚡ Quick install

One-liner (run on **both** servers):

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/pdnczone/hashem-panel/main/install.sh)
```

Manual:

```bash
git clone https://github.com/pdnczone/hashem-panel.git
cd hashem-panel
sudo bash hashem.sh
```

### Setup flow (single `hsh1_...` bundle)

1. **Iran first:** option `1` — Iran + foreign public IPs, FRP port (default `7000`). The panel and the script print a **setup bundle** like `hsh1_85.1.2.3_7000_10.10.10.2_10.10.10.1_<token>_443-2083` (IP + control port + GRE pair + token + ports in one pasteable string).
2. **Foreign next:** option `2` — paste the bundle (fills everything incl. ports) **or** enter Iran IP + port + legacy 32-char token + ports (`443, 2083, 8080`) manually.
3. **Check:** option `6` — GRE status, inner ping, FRP service state.
4. Panel installs automatically; credentials print at the end (also option `13` anytime).

> **فارسی:** اول روی ایران گزینه `1` (باندل `hsh1_...` رو کپی کن — همه‌چیز توشه)، بعد روی خارج گزینه `2` (باندل رو paste کن، همه فیلدها خودش پر می‌شه). تست با گزینه `6`. پنل وب خودکار نصب می‌شه.

---

## ⌨️ `hashem` command

After install, just type in SSH:

```bash
hashem            # full interactive menu, grouped: Setup · Peers · Monitor · Tune · Panel & System (0-18)
```

Non-interactive (same flags as `hashem.sh`):

```bash
hashem status | logs | restart | show-panel-url
hashem update                                   # latest script + latest prebuilt panel
hashem setup-iran    --local-pub IP --remote-pub IP [--frp-port N] [--token T] [--force]
hashem setup-foreign --bundle hsh1_... [--force]   # one paste fills everything; explicit flags win
hashem setup-foreign --local-pub IP --remote-pub IP --token T --ports "443, 2083" [--force]
hashem add-peer --bundle hsh1_... [--name LABEL]   # or full flags below
hashem add-peer --local-pub IP --remote-pub IP --frp-port N --token T \
  --local-gre IP --peer-gre IP --ports "443, 2083" [--name LABEL]
hashem remove-peer --id N [--force] | peer-list | peer-token --id N
hashem panel-tls [domain] [email]     # Let's Encrypt for the web panel
hashem optimize | restore | tune-status
```

`grepanel` (panel service control) still works side by side:

```bash
grepanel status | logs | restart | url | password | uninstall
```

---

## 🖥️ Web panel

After setup it auto-installs and prints:

```
Panel URL:  http://<server-ip>:7777/<secret-path>
Username:   admin
Password:   8-digit number
```

| Tab | What it does |
|-----|--------------|
| Dashboard | Traffic chart (24H/7D/30D), KPIs, Details (Server/GRE/FRP) + **Peer Tunnels summary** — click a row to jump to its full card |
| Tunnel | **Main tunnel card** (gold accent) + per-peer cards (Foreign + Iran IP, GRE pair, control port, traffic, copy buttons), Ping/Bundle/Restart/Remove per card, inline two-step Remove |
| Setup | Quick Setup 1-2-3 + `hsh1_...` bundle box (one paste configures Foreign), add-peer, overwrite-guarded, port-clash warnings |
| Logs | Per-peer FRP logs, live tail, panel errors with `E-XXXX` codes |
| Update | One-click update to the latest `panel-rN` release |
| Settings | Password, panel HTTPS (Let's Encrypt), terminal flag |
| Terminal | Real root shell (enable in Settings first), quick `hashem` buttons |

After login you can re-run the whole tunnel setup from the browser — same logic as the script, step-by-step log included. Dark/light switch in the sidebar.

### Screenshots

![Dashboard — KPIs, traffic chart, Details + Peer Tunnels summary](docs/dashboard.png)

![Dashboard Details + Peer Tunnels summary rows](docs/dashboard-details.png)

![Tunnel — Main card + per-peer cards with Ping/Bundle/Restart/Remove](docs/tunnel.png)

---

## 📋 Menu reference

| Option | Group | What it does |
|--------|-------|--------------|
| 1 | Setup | Setup IRAN (GRE + `frps`) + auto-install panel |
| 2 | Setup | Setup FOREIGN (GRE + `frpc` reverse) + auto-install panel |
| 3 | Peers | Add peer tunnel (Iran: another foreign server) |
| 4 | Peers | List peer tunnels |
| 5 | Peers | Remove peer tunnel |
| 6 | Monitor | Status + GRE ping test |
| 7 | Monitor | Live FRP logs |
| 8 | Monitor | Restart tunnel services |
| 9 | Monitor | Remove tunnel (GRE + FRP gone, **panel stays**) |
| 10 | Tune | Optimize tunnel (BBR + buffers + MTU/MSS, with backup) |
| 11 | Tune | Restore pre-optimize settings |
| 12 | Tune | Optimization status |
| 13 | Panel & System | Show panel URL + username + password |
| 14 | Panel & System | Panel HTTPS (Let's Encrypt certificate) |
| 15 | Panel & System | Update all (latest script + latest prebuilt panel) |
| 16 | Panel & System | Free RAM (journald cap + drop cache + 1GB swap) |
| 17 | Panel & System | CLI help (non-interactive commands) |
| 18 | Panel & System | Uninstall everything (services + interface + binaries) |
| 0 | — | Exit |

---

## 🔧 Troubleshooting

| Symptom | Likely cause → fix |
|---------|-------------------|
| `Panel URL` empty / 404 | Secret path rotated after reinstall → option `13` prints the current one |
| `E-AUTH-01` right after login | Stale session cookie — hard refresh and log in again |
| `frpc dial 127.0.0.1:443 refused` | Nothing listens on 443 on foreign — point frpc at the real local port |
| `port unavailable` (e.g. 8080) | Something already listens there (`ss -tlnp \| grep 8080`) — pick another port or stop it |
| FRP up but service unreachable | Check `4` (logs), `3` (GRE ping), token match on both sides, GRE proto 47 open |
| Old binary after option `15` | GitHub `latest` redirect cache — re-run `15`; it verifies ELF before installing |

Requirements: Linux + `systemd`, `root`, GRE (protocol 47) open between servers.

---

## 🔐 Security note

⚠️ The panel password is also saved in plaintext at `/etc/gre-panel/panel.pass` (mode `600`) so option `13` can show it — convenient, not maximally secure. Anyone with root on the server can read it.

- Change it anytime: panel **Settings** tab, `grepanel password`, or option `13` auto-regenerates if missing.
- Panel listens on `:7777` (and `:7443` with a cert) — restrict with firewall to your IP if exposed.
- Enable HTTPS (option `16` or Settings → Panel HTTPS) on any panel reachable from the internet.

> **فارسی:** پسورد پنل به‌صورت متنی در `panel.pass` ذخیره می‌شه تا گزینه `8` نشونش بده (انتخاب آگاهانه برای راحتی). هر وقت خواستی از تب Settings عوضش کن، سرتیفیکیت بگیر و پورت پنل رو با فایروال محدود کن.

---

## 📁 Repo layout

```
hashem.sh               # everything: setup iran/foreign, peers, status, logs, update, panel, TLS (`hashem` = same file, installed)
install.sh              # one-liner entry → hashem.sh
panel/                  # Go single-binary web panel
  main.go               # routes, HTTP+HTTPS listeners, feature flags
  terminal.go           # xterm.js + WebSocket + PTY interactive shell
  tls.go                # Let's Encrypt issue/renew + HTTPS serve
  setup.go / tunnel.go  # setup + peers API, status, logs
  dashboard.go / errors.go  # metrics, E-XXXX catalog
  index.html            # all 7 tabs (vanilla JS + enterprise DS)
  grepanel              # server-side panel service control (delegates tune/peer cmds to hashem.sh)
  README.md             # panel walkthrough
.github/workflows/     # build-panel.yml → prebuilt panel-rN releases
spoof_test.py           # spoof test helper (direct vs tunneled)
```

---

## 🤝 Contributing

PRs and issues are welcome. For big features, open an issue first so we agree on the design before code.

### 👥 Contributors

- **PDNC** — [@pdnczone](https://github.com/pdnczone) · [🌐 pdnczone.ir](https://pdnczone.ir) · [✈️ Telegram](https://t.me/pdnczone) · [▶️ YouTube](https://youtube.com/@pdnczone)

---

## 🔗 Links

- 🌐 Website: [pdnczone.ir](https://pdnczone.ir)
- ✈️ Telegram: [@pdnczone](https://t.me/pdnczone)
- ▶️ YouTube: [@pdnczone](https://youtube.com/@pdnczone)
