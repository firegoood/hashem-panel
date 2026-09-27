#!/bin/bash

# ==============================================================================
#   Hashem — GRE + FRP Reverse Tunnel Automated Setup Script (hashem.sh)
#   Architecture: GRE Layer 3 Tunnel + FRP Reverse TLS Tunnel
#   Features: Auto Arch Detect, Systemd Auto-start on boot, MTU Clamping, TCP/UDP
#   One file: interactive menu (`bash hashem.sh`) + non-interactive CLI
#   (`hashem setup-iran ...`) — the old gre.sh name still works as symlink.
# ==============================================================================
# ---- installed names (single source of truth for this script) ----
HASHEM_BIN="/usr/local/bin/hashem"       # this script, after install
HASHEM_SCRIPT="/usr/local/bin/hashem.sh" # versioned copy (gre.sh = legacy alias)
HASHEM_URL_BASE="https://raw.githubusercontent.com/pdnczone/hashem-panel/main"

CYAN='\033[0;36m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/frp"
DEFAULT_FRP_VERSION="0.71.0"

# Default GRE internal IPs (/30 subnet)
IRAN_GRE_IP="10.10.10.2"
FOREIGN_GRE_IP="10.10.10.1"
TUNNEL_NAME="gre-tunnel"
WATCHDOG_FILE="/etc/gre-panel/watchdog.json"
PERF_FILE="/etc/gre-panel/perf.json"
BACKUP_DIR="/var/backups/hashem"

# ---- Performance / Obfuscation Configuration (/etc/gre-panel/perf.json) ----
init_perf_json() {
    mkdir -p /etc/gre-panel
    if [[ ! -f "$PERF_FILE" ]]; then
        cat << 'EOF' > "$PERF_FILE"
{
  "proxy_encryption": false,
  "proxy_compression": false,
  "force_tls": true,
  "chaff_profile": "low",
  "dpi_enabled": true,
  "dpi_rate": "300/min",
  "dpi_burst": 100
}
EOF
        chmod 600 "$PERF_FILE" 2>/dev/null || true
    fi
}

perf_get_enc() {
    if [[ -n "${PERF_ENC:-}" ]]; then
        [[ "$PERF_ENC" == "1" || "$PERF_ENC" == "true" ]] && echo 1 || echo 0
        return 0
    fi
    if [[ -f "$PERF_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        python3 -c '
import json
try:
    with open("'"$PERF_FILE"'") as f:
        print(1 if json.load(f).get("proxy_encryption", False) else 0)
except Exception:
    print(0)
' 2>/dev/null && return 0
    elif [[ -f "$PERF_FILE" ]]; then
        grep -q '"proxy_encryption"[[:space:]]*:[[:space:]]*true' "$PERF_FILE" && echo 1 || echo 0
        return 0
    fi
    echo 0
}

perf_get_comp() {
    if [[ -n "${PERF_COMP:-}" ]]; then
        [[ "$PERF_COMP" == "1" || "$PERF_COMP" == "true" ]] && echo 1 || echo 0
        return 0
    fi
    if [[ -f "$PERF_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        python3 -c '
import json
try:
    with open("'"$PERF_FILE"'") as f:
        print(1 if json.load(f).get("proxy_compression", False) else 0)
except Exception:
    print(0)
' 2>/dev/null && return 0
    elif [[ -f "$PERF_FILE" ]]; then
        grep -q '"proxy_compression"[[:space:]]*:[[:space:]]*true' "$PERF_FILE" && echo 1 || echo 0
        return 0
    fi
    echo 0
}

perf_get_tls() {
    if [[ -n "${PERF_TLS:-}" ]]; then
        [[ "$PERF_TLS" == "1" || "$PERF_TLS" == "true" ]] && echo 1 || echo 0
        return 0
    fi
    if [[ -f "$PERF_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        python3 -c '
import json
try:
    with open("'"$PERF_FILE"'") as f:
        print(1 if json.load(f).get("force_tls", True) else 0)
except Exception:
    print(1)
' 2>/dev/null && return 0
    elif [[ -f "$PERF_FILE" ]]; then
        grep -q '"force_tls"[[:space:]]*:[[:space:]]*false' "$PERF_FILE" && echo 0 || echo 1
        return 0
    fi
    echo 1
}

perf_get_chaff() {
    if [[ -n "${CHAFF_PROFILE:-}" ]]; then
        echo "$CHAFF_PROFILE"
        return 0
    fi
    if [[ -f "$PERF_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        python3 -c '
import json
try:
    with open("'"$PERF_FILE"'") as f:
        p = json.load(f).get("chaff_profile", "low")
        print(p if p in ("off", "low", "mid") else "low")
except Exception:
    print("low")
' 2>/dev/null && return 0
    fi
    echo "low"
}

perf_get_dpi_enabled() {
    if [[ -f "$PERF_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        python3 -c '
import json
try:
    with open("'"$PERF_FILE"'") as f:
        print(1 if json.load(f).get("dpi_enabled", True) else 0)
except Exception:
    print(1)
' 2>/dev/null && return 0
    elif [[ -f "$PERF_FILE" ]]; then
        grep -q '"dpi_enabled"[[:space:]]*:[[:space:]]*false' "$PERF_FILE" && echo 0 || echo 1
        return 0
    fi
    echo 1
}

perf_get_dpi_rate() {
    if [[ -f "$PERF_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        python3 -c '
import json
try:
    with open("'"$PERF_FILE"'") as f:
        r = json.load(f).get("dpi_rate", "300/min")
        print(r if r else "300/min")
except Exception:
    print("300/min")
' 2>/dev/null && return 0
    fi
    echo "300/min"
}

perf_get_dpi_burst() {
    if [[ -f "$PERF_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        python3 -c '
import json
try:
    with open("'"$PERF_FILE"'") as f:
        b = json.load(f).get("dpi_burst", 100)
        print(int(b) if int(b) > 0 else 100)
except Exception:
    print(100)
' 2>/dev/null && return 0
    fi
    echo 100
}

perf_set_val() {
    local key="$1" val="$2" is_raw="${3:-0}"
    init_perf_json
    if command -v python3 >/dev/null 2>&1; then
        python3 -c '
import json
path = "'"$PERF_FILE"'"
key = "'"$key"'"
raw = '"$is_raw"'
val_str = """'"$val"'"""
try:
    with open(path, "r") as f:
        d = json.load(f)
except Exception:
    d = {}
if raw:
    if val_str in ("true", "True", "1"):
        d[key] = True
    elif val_str in ("false", "False", "0"):
        d[key] = False
    else:
        try:
            d[key] = int(val_str)
        except Exception:
            d[key] = val_str
else:
    d[key] = val_str
with open(path, "w") as f:
    json.dump(d, f, indent=2)
'
        chmod 600 "$PERF_FILE" 2>/dev/null || true
    fi
}

# ---- input validation (same rules as the web panel: IPv4, port 1-65535) ----
is_valid_ip() {
    [[ "$1" =~ ^([0-9]{1,3}\.){3}[0-9]{1,3}$ ]] || return 1
    local IFS=. a b c d o
    read -r a b c d <<<"$1"
    for o in "$a" "$b" "$c" "$d"; do
        ((10#$o <= 255)) || return 1
    done
}

is_valid_port() {
    [[ "$1" =~ ^[0-9]+$ ]] && ((10#$1 >= 1 && 10#$1 <= 65535))
}

panel_tls_issue() { # $1=domain [$2=email] — certbot standalone on :80 + install to /etc/gre-panel/tls
    local DOMAIN=${1:-} EMAIL=${2:-}
    [[ -z "$DOMAIN" ]] && read -p "Panel domain (e.g. panel.example.com, must point to this server): " DOMAIN
    [[ -z "$DOMAIN" ]] && { echo -e "${RED}[!] Domain is required.${NC}"; return 1; }
    read -p "Email for expiry notices [Enter to skip]: " EMAIL_IN
    EMAIL=${EMAIL:-$EMAIL_IN}
    if ! command -v certbot >/dev/null 2>&1; then
        echo -e "${CYAN}[*] Installing certbot...${NC}"
        apt-get update -qq && apt-get install -y -qq certbot || { echo -e "${RED}[!] certbot install failed.${NC}"; return 1; }
    fi
    echo -e "${CYAN}[*] Issuing Let's Encrypt certificate for ${DOMAIN} (needs port 80 free + DNS pointing here)...${NC}"
    local ARGS=(certonly --standalone --non-interactive --agree-tos --preferred-challenges http --http-01-port 80 -d "$DOMAIN")
    if [[ -n "$EMAIL" ]]; then ARGS+=(-m "$EMAIL"); else ARGS+=(--register-unsafely-without-email); fi
    if ! certbot "${ARGS[@]}"; then
        echo -e "${RED}[!] certbot failed — check DNS (domain → this server IP) and that port 80 is reachable.${NC}"
        return 1
    fi
    mkdir -p /etc/gre-panel/tls
    cp "/etc/letsencrypt/live/${DOMAIN}/fullchain.pem" /etc/gre-panel/tls/server.crt
    cp "/etc/letsencrypt/live/${DOMAIN}/privkey.pem" /etc/gre-panel/tls/server.key
    chmod 600 /etc/gre-panel/tls/server.key
    echo "{\"domain\":\"$DOMAIN\",\"issued_at\":\"$(date '+%F %T')\"}" > /etc/gre-panel/tls/meta.json
    systemctl restart gre-panel
    sleep 2
    local PORT BASE
    PORT=$(grep -o '"port": *[0-9]*' /etc/gre-panel/panel.json 2>/dev/null | grep -o '[0-9]*'); PORT=${PORT:-7777}
    BASE=$(grep -o '"base_path": *"[^"]*"' /etc/gre-panel/panel.json 2>/dev/null | cut -d'"' -f4)
    local TPORT
    TPORT=$(grep -o '"tls_port": *[0-9]*' /etc/gre-panel/panel.json 2>/dev/null | grep -o '[0-9]*'); TPORT=${TPORT:-7443}
    echo -e "${GREEN}[✔️] HTTPS ready: ${CYAN}https://${DOMAIN}:${TPORT}/${BASE}${NC}"
    echo -e "${GREEN}    HTTP still works: ${CYAN}http://<this-server-ip>:${PORT}/${BASE}${NC}"
    echo -e "${CYAN}[*] certbot auto-renews via its systemd timer; panel shows expiry in Settings.${NC}"
}

gen_token32() { # 32-char alphanumeric secret (FRP auth token)
    tr -dc A-Za-z0-9 </dev/urandom | head -c 32 2>/dev/null || openssl rand -hex 16
}

gen_random_port() { # random port 20000-60000 for FRP
    if command -v shuf >/dev/null 2>&1; then
        shuf -i 20000-60000 -n 1
    elif command -v python3 >/dev/null 2>&1; then
        python3 -c 'import random; print(random.randint(20000, 60000))'
    else
        awk 'BEGIN{srand(); print int(20000 + rand() * 40001)}'
    fi
}

# ---- setup bundle: one readable string with everything foreign needs ----
# Format: hsh1_<IRAN_PUB>_<FRP_PORT>_<IRAN_GRE>_<FOREIGN_GRE>_<TOKEN>[_<PORTS>]
# PORTS optional, dash-separated (443-2083). Legacy 32-char tokens (no hsh1_
# prefix) keep working everywhere — bundle_parse rejects them, callers fall
# back to manual fields.
BUNDLE_PREFIX="hsh1_"
bundle_make() { # $1=iran_pub $2=frp_port $3=iran_gre $4=foreign_gre $5=token [$6="p1 p2"]
    local IRAN_PUB=$1 FRP_PORT=$2 IRAN_GRE=$3 FOREIGN_GRE=$4 TOKEN=$5 PORTS_SP=${6:-}
    local PORTS_DASH=""
    if [[ -n "$PORTS_SP" ]]; then
        PORTS_DASH=$(echo "$PORTS_SP" | xargs | tr ' ' '-')
    fi
    if [[ -n "$PORTS_DASH" ]]; then
        echo "${BUNDLE_PREFIX}${IRAN_PUB}_${FRP_PORT}_${IRAN_GRE}_${FOREIGN_GRE}_${TOKEN}_${PORTS_DASH}"
    else
        echo "${BUNDLE_PREFIX}${IRAN_PUB}_${FRP_PORT}_${IRAN_GRE}_${FOREIGN_GRE}_${TOKEN}"
    fi
}
# bundle_parse $1: sets B_IRAN_PUB B_FRP_PORT B_IRAN_GRE B_FOREIGN_GRE B_TOKEN
# B_PORTS (space-separated, may be empty). Returns 0 on valid bundle.
bundle_parse() {
    B_IRAN_PUB=""; B_FRP_PORT=""; B_IRAN_GRE=""; B_FOREIGN_GRE=""; B_TOKEN=""; B_PORTS=""
    local IN=$1 rest a b c d e f
    [[ "$IN" == ${BUNDLE_PREFIX}* ]] || return 1
    rest=${IN#${BUNDLE_PREFIX}}
    IFS=_ read -r a b c d e f <<<"$rest"
    [[ -n "$a" && -n "$b" && -n "$c" && -n "$d" && -n "$e" ]] || return 1
    is_valid_ip "$a" || return 1
    is_valid_port "$b" || return 1
    is_valid_ip "$c" || return 1
    is_valid_ip "$d" || return 1
    [[ ${#e} -ge 1 && ${#e} -le 128 ]] || return 1
    local CLEANED="" p
    if [[ -n "${f:-}" ]]; then
        for p in $(echo "$f" | tr -- '-,' '  '); do
            is_valid_port "$p" && CLEANED="$CLEANED $((10#$p))"
        done
        CLEANED=$(echo "$CLEANED" | xargs)
        [[ -n "$CLEANED" ]] || return 1
    fi
    B_IRAN_PUB=$a; B_FRP_PORT=$((10#$b)); B_IRAN_GRE=$c; B_FOREIGN_GRE=$d; B_TOKEN=$e; B_PORTS=$CLEANED
    return 0
}
prompt_ip() { # $1=varname $2=label $3=default (empty = required)
    local __var=$1 __label=$2 __def=$3 __in
    while true; do
        if [[ -n "$__def" ]]; then
            read -p "$__label [Default: $__def]: " __in
            __in=${__in:-$__def}
        else
            read -p "$__label: " __in
        fi
        if is_valid_ip "$__in"; then printf -v "$__var" '%s' "$__in"; return 0; fi
        echo -e "${RED}[!] Invalid IPv4 address: '${__in}'. Example: 203.0.113.10${NC}"
    done
}

prompt_port() { # $1=varname $2=label $3=default
    local __var=$1 __label=$2 __def=$3 __in
    while true; do
        read -p "$__label [Default: $__def]: " __in
        __in=${__in:-$__def}
        if is_valid_port "$__in"; then printf -v "$__var" '%s' "$((10#$__in))"; return 0; fi
        echo -e "${RED}[!] Invalid port: '${__in}'. Must be 1-65535.${NC}"
    done
}

prompt_required() { # $1=varname $2=label — must be non-empty
    local __var=$1 __label=$2 __in
    while true; do
        read -p "$__label: " __in
        if [[ -n "$__in" ]]; then printf -v "$__var" '%s' "$__in"; return 0; fi
        echo -e "${RED}[!] This field is required and cannot be empty.${NC}"
    done
}

prompt_token() { # $1=varname $2=label $3=default (empty accepts default)
    local __var=$1 __label=$2 __def=$3 __in
    read -p "$__label [Press Enter for: $__def]: " __in
    printf -v "$__var" '%s' "${__in:-$__def}"
}

prompt_ports() { # $1=varname $2=label — at least one valid port
    local __var=$1 __label=$2 __in __ok p
    while true; do
        read -p "$__label (e.g. 443, 2083, 8080): " __in
        __ok=""
        for p in $(echo "$__in" | tr ',' ' '); do
            is_valid_port "$p" && __ok="$__ok $((10#$p))"
        done
        __ok=$(echo "$__ok" | xargs)
        if [[ -n "$__ok" ]]; then printf -v "$__var" '%s' "$__ok"; return 0; fi
        echo -e "${RED}[!] Enter at least one valid port (1-65535).${NC}"
    done
}

# validate_setup_common checks non-interactive args with the same rules as
# the prompts above. Prints a clear error per bad field, returns non-zero.
validate_setup_common() { # $1=local_pub $2=remote_pub $3=frp_port $4=local_gre
    local ok=1
    is_valid_ip "$1" || { echo -e "${RED}[!] Invalid local public IP: '$1'${NC}"; ok=0; }
    is_valid_ip "$2" || { echo -e "${RED}[!] Invalid remote public IP: '$2'${NC}"; ok=0; }
    is_valid_port "$3" || { echo -e "${RED}[!] Invalid FRP port: '$3' (must be 1-65535)${NC}"; ok=0; }
    is_valid_ip "$4" || { echo -e "${RED}[!] Invalid local GRE IP: '$4'${NC}"; ok=0; }
    return $((1 - ok))
}

tunnel_present() {
    ip tunnel show 2>/dev/null | grep -q "$TUNNEL_NAME" && return 0
    [[ -f "${CONFIG_DIR}/frps.toml" || -f "${CONFIG_DIR}/frpc.toml" ]] && return 0
    return 1
}

check_root() {
    if [[ $EUID -ne 0 ]]; then
        echo -e "${RED}[!] This script must be run as root (sudo).${NC}"
        exit 1
    fi
}

detect_arch() {
    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)
            FRP_ARCH="amd64"
            ;;
        aarch64|arm64)
            FRP_ARCH="arm64"
            ;;
        armv7l|armhf)
            FRP_ARCH="arm"
            ;;
        *)
            echo -e "${RED}[!] Unsupported architecture: $ARCH${NC}"
            exit 1
            ;;
    esac
}

get_latest_frp_version() {
    LATEST_VER=$(curl -sSL --max-time 5 "https://api.github.com/repos/fatedier/frp/releases/latest" 2>/dev/null | grep '"tag_name":' | sed -E 's/.*"v([^"]+)".*/\1/')
    if [[ -z "$LATEST_VER" ]]; then
        FRP_VERSION="$DEFAULT_FRP_VERSION"
    else
        FRP_VERSION="$LATEST_VER"
    fi
}

install_frp_binaries() {
    # already installed → reuse (add-peer must not re-download FRP per peer,
    # and must never exit the caller if the network is slow — peers 2..5
    # would otherwise fail with E-INSTALL-02 on a healthy machine).
    if [[ -x "${INSTALL_DIR}/frps" && -x "${INSTALL_DIR}/frpc" ]]; then
        return 0
    fi
    detect_arch
    get_latest_frp_version
    echo -e "${CYAN}[*] Downloading FRP v${FRP_VERSION} (${FRP_ARCH})...${NC}"

    mkdir -p "$CONFIG_DIR"
    TMP_DIR=$(mktemp -d)
    TAR_FILE="frp_${FRP_VERSION}_linux_${FRP_ARCH}.tar.gz"
    DOWNLOAD_URL="https://github.com/fatedier/frp/releases/download/v${FRP_VERSION}/${TAR_FILE}"

    if ! curl -fsSL --max-time 90 -o "${TMP_DIR}/${TAR_FILE}" "$DOWNLOAD_URL"; then
        echo -e "${RED}[!] Failed to download FRP from GitHub.${NC}"
        rm -rf "$TMP_DIR"
        return 1
    fi

    tar -xzf "${TMP_DIR}/${TAR_FILE}" -C "$TMP_DIR"
    EXTRACTED_DIR="${TMP_DIR}/frp_${FRP_VERSION}_linux_${FRP_ARCH}"

    cp "${EXTRACTED_DIR}/frps" "$INSTALL_DIR/" 2>/dev/null
    cp "${EXTRACTED_DIR}/frpc" "$INSTALL_DIR/" 2>/dev/null
    chmod +x "${INSTALL_DIR}/frps" "${INSTALL_DIR}/frpc"

    rm -rf "$TMP_DIR"
    echo -e "${GREEN}[✔️] FRP installed to ${INSTALL_DIR}.${NC}"
}

setup_gre_systemd() {
    setup_gre_iface "$TUNNEL_NAME" "$1" "$2" "$3"
}

# Generalized GRE interface setup: $1=ifname $2=local_pub $3=remote_pub $4=inner_ip.
# setup_gre_systemd() above is the legacy single-tunnel wrapper; peers call this
# directly with gre-tN names so every tunnel is the same GRE, just N of them.
setup_gre_iface() {
    local IFNAME=$1
    local LOCAL_IP=$2
    local REMOTE_IP=$3
    local GRE_INTERNAL_IP=$4

    echo -e "${CYAN}[*] Configuring persistent GRE tunnel service (${IFNAME})...${NC}"

    # Tear down existing if present
    ip tunnel del "$IFNAME" >/dev/null 2>&1 || true

    # Create systemd service for GRE
    cat <<EOF > /etc/systemd/system/${IFNAME}.service
[Unit]
Description=GRE Tunnel Interface
After=network.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStartPre=-/sbin/ip tunnel del ${IFNAME}
ExecStart=/bin/sh -c "/sbin/ip tunnel add ${IFNAME} mode gre local ${LOCAL_IP} remote ${REMOTE_IP} ttl 255 && /sbin/ip link set dev ${IFNAME} up mtu 1448 && /sbin/ip addr add ${GRE_INTERNAL_IP}/30 dev ${IFNAME}"
ExecStop=-/sbin/ip tunnel del ${IFNAME}

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable "${IFNAME}.service" >/dev/null 2>&1
    if ! systemctl restart "${IFNAME}.service"; then
        echo -e "${RED}[!] GRE interface ${IFNAME} failed to start — check: ip tunnel show; journalctl -u ${IFNAME}.service${NC}"
        return 1
    fi

    # Enable packet forwarding & MSS clamping to avoid fragmentation
    sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
    iptables -t mangle -C POSTROUTING -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu >/dev/null 2>&1 || \
        iptables -t mangle -A POSTROUTING -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu

    echo -e "${GREEN}[✔️] GRE Tunnel service active with IP ${GRE_INTERNAL_IP}.${NC}"
}

# ---- Traffic Obfuscation / Chaff Service (idle gap filler) ----
CHAFF_BIN="/usr/local/bin/hashem-chaff.sh"

install_chaff_script() {
    cat <<'EOF' > "$CHAFF_BIN"
#!/usr/bin/env bash
# /usr/local/bin/hashem-chaff.sh - GRE tunnel idle-gap chaff generator

PEER_IP="${1:-}"
if [[ -z "$PEER_IP" ]]; then
    echo "Usage: $0 <peer_inner_ip> [low|mid]" >&2
    exit 1
fi

PROFILE="${2:-${CHAFF_PROFILE:-low}}"

trap 'exit 0' SIGTERM SIGINT

while true; do
    if [[ "$PROFILE" == "mid" ]]; then
        # mid: intervals 0.15-1.2s, size 200-1400
        ms=$(( 150 + RANDOM % 1051 ))
        sleep_sec=$(printf "%d.%03d" $((ms / 1000)) $((ms % 1000)))
        size=$(( 200 + RANDOM % 1201 ))
    else
        # low (default): intervals 0.4-2.8s, size 64-1200
        ms=$(( 400 + RANDOM % 2401 ))
        sleep_sec=$(printf "%d.%03d" $((ms / 1000)) $((ms % 1000)))
        size=$(( 64 + RANDOM % 1137 ))
    fi

    sleep "$sleep_sec"

    # 16 random hex bytes (32 hex characters)
    pattern=$(printf '%04x%04x%04x%04x%04x%04x%04x%04x' $RANDOM $RANDOM $RANDOM $RANDOM $RANDOM $RANDOM $RANDOM $RANDOM)

    ping -c1 -W1 -s "$size" -p "$pattern" "$PEER_IP" >/dev/null 2>&1 || true
done
EOF
    chmod +x "$CHAFF_BIN"
}

# setup_chaff: $1=ifname_suffix("" for legacy, "-N" for peers) $2=peer_gre_ip
setup_chaff() {
    local SUF=$1 PEER_GRE=$2
    local PROFILE="${CHAFF_PROFILE:-$(perf_get_chaff)}"
    if [[ "$PROFILE" == "off" ]]; then
        return 0
    fi
    is_valid_ip "$PEER_GRE" || return 1
    install_chaff_script || return 1

    local SVC="gre-chaff"
    local GRE_IF="$TUNNEL_NAME"
    if [[ -n "$SUF" ]]; then
        local ID="${SUF#-}"
        SVC="gre-chaff-${ID}"
        GRE_IF="gre-t${ID}"
    fi

    local AFTER_GRE=""
    if [[ -f "/etc/systemd/system/${GRE_IF}.service" ]]; then
        AFTER_GRE=" ${GRE_IF}.service"
    fi

    cat <<EOF > "/etc/systemd/system/${SVC}.service"
[Unit]
Description=GRE Tunnel Chaff Service (idle gap filler)${SUF:+ (peer${SUF#-})}
After=network.target${AFTER_GRE}
${AFTER_GRE:+Wants=${GRE_IF}.service}

[Service]
Type=simple
User=root
Restart=always
RestartSec=3s
ExecStart=${CHAFF_BIN} ${PEER_GRE} ${PROFILE}

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable "${SVC}.service" >/dev/null 2>&1
    systemctl restart "${SVC}.service" >/dev/null 2>&1 || true
    echo -e "${GREEN}[✔️] Chaff service ${SVC} configured for peer ${PEER_GRE} (profile: ${PROFILE}).${NC}"
}

update_chaff_existing_tunnels() {
    if [[ "${CHAFF_PROFILE:-low}" == "off" ]]; then
        return 0
    fi
    # 1. Multi-peer registry (/etc/gre-panel/peers.json)
    if [[ -f "$PEERS_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        local PEER_DATA
        PEER_DATA=$(PEERS_F="$PEERS_FILE" python3 -c '
import json, os
try:
    d = json.load(open(os.environ["PEERS_F"]))
    for p in d.get("peers", []):
        suf = "" if p.get("legacy") else f"-{p.get(\"id\", \"\")}"
        pgre = p.get("peer_gre", "")
        prof = p.get("chaff_profile", "")
        if pgre:
            print(f"{suf}:{pgre}:{prof}")
except Exception:
    pass
' 2>/dev/null)
        if [[ -n "$PEER_DATA" ]]; then
            while IFS=':' read -r suf pgre prof; do
                [[ -n "$pgre" ]] || continue
                local saved_prof="${CHAFF_PROFILE:-}"
                [[ -n "$prof" ]] && CHAFF_PROFILE="$prof"
                setup_chaff "$suf" "$pgre"
                CHAFF_PROFILE="$saved_prof"
            done <<< "$PEER_DATA"
            return 0
        fi
    fi

    # 2. Foreign server (/etc/frp/frpc.toml)
    if [[ -f "${CONFIG_DIR}/frpc.toml" ]]; then
        local PEER_GRE
        PEER_GRE=$(grep -E '^[[:space:]]*serverAddr[[:space:]]*=' "${CONFIG_DIR}/frpc.toml" | cut -d'=' -f2 | tr -d ' "' | tr -d " \t\r\n")
        if is_valid_ip "$PEER_GRE"; then
            setup_chaff "" "$PEER_GRE"
            return 0
        fi
    fi

    # 3. Legacy Iran server (/etc/systemd/system/gre-tunnel.service or /etc/frp/frps.toml)
    if [[ -f "/etc/systemd/system/${TUNNEL_NAME}.service" || -f "${CONFIG_DIR}/frps.toml" ]]; then
        local INNER_IP=""
        if [[ -f "/etc/systemd/system/${TUNNEL_NAME}.service" ]]; then
            INNER_IP=$(grep -oE 'addr add [0-9]+\.[0-9]+\.[0-9]+\.[0-9]+' "/etc/systemd/system/${TUNNEL_NAME}.service" | awk '{print $3}' | head -1)
        fi
        if [[ -z "$INNER_IP" ]] && ip addr show "$TUNNEL_NAME" >/dev/null 2>&1; then
            INNER_IP=$(ip addr show "$TUNNEL_NAME" 2>/dev/null | awk '/inet / {print $2}' | cut -d/ -f1 | head -1)
        fi
        if is_valid_ip "$INNER_IP"; then
            local last=${INNER_IP##*.}; local prefix=${INNER_IP%.*}
            if (( last % 2 == 0 )); then last=$((last - 1)); else last=$((last + 1)); fi
            local P_GRE="${prefix}.${last}"
            if is_valid_ip "$P_GRE"; then
                setup_chaff "" "$P_GRE"
            fi
        fi
    fi
}

cli_chaff() {
    local ACTION="${1:-status}"
    case "$ACTION" in
        on)
            echo -e "${CYAN}[*] Enabling and starting GRE chaff services...${NC}"
            local found=0
            for u in /etc/systemd/system/gre-chaff*.service; do
                [[ -f "$u" ]] || continue
                found=1
                local bname
                bname=$(basename "$u")
                systemctl enable "$bname" >/dev/null 2>&1
                systemctl restart "$bname" >/dev/null 2>&1
                echo -e "${GREEN}[✔️] Started and enabled ${bname}.${NC}"
            done
            if [[ "$found" -eq 0 ]]; then
                echo -e "${YELLOW}[*] No existing chaff services found — configuring for active tunnels...${NC}"
                update_chaff_existing_tunnels
            fi
            ;;
        off)
            echo -e "${CYAN}[*] Stopping and disabling GRE chaff services...${NC}"
            local found=0
            for u in /etc/systemd/system/gre-chaff*.service; do
                [[ -f "$u" ]] || continue
                found=1
                local bname
                bname=$(basename "$u")
                systemctl stop "$bname" >/dev/null 2>&1
                systemctl disable "$bname" >/dev/null 2>&1
                echo -e "${GREEN}[✔️] Stopped and disabled ${bname}.${NC}"
            done
            if [[ "$found" -eq 0 ]]; then
                echo -e "${YELLOW}[*] No chaff services found.${NC}"
            fi
            ;;
        status)
            echo -e "${CYAN}=== GRE Chaff (Traffic Obfuscation) Status ===${NC}"
            echo -e "${YELLOW}Notice: Fills idle gaps to break timing analysis; does not hide volume under load.${NC}"
            local found=0
            for u in /etc/systemd/system/gre-chaff*.service; do
                [[ -f "$u" ]] || continue
                found=1
                local bname
                bname=$(basename "$u")
                local active enabled exec_line peer_ip prof
                active=$(systemctl is-active "$bname" 2>/dev/null)
                [[ -z "$active" ]] && active="inactive"
                enabled=$(systemctl is-enabled "$bname" 2>/dev/null)
                [[ -z "$enabled" ]] && enabled="disabled"
                exec_line=$(grep -E '^[[:space:]]*ExecStart[[:space:]]*=' "$u" | head -1)
                peer_ip=$(echo "$exec_line" | awk '{print $2}')
                prof=$(echo "$exec_line" | awk '{print $3}')
                prof=${prof:-low}
                if [[ "$active" == "active" ]]; then
                    echo -e "  ${bname}: ${GREEN}ACTIVE${NC} (${enabled}) | peer: ${CYAN}${peer_ip}${NC} | profile: ${YELLOW}${prof}${NC}"
                else
                    echo -e "  ${bname}: ${RED}${active}${NC} (${enabled}) | peer: ${CYAN}${peer_ip}${NC} | profile: ${YELLOW}${prof}${NC}"
                fi
            done
            if [[ "$found" -eq 0 ]]; then
                echo -e "${YELLOW}[*] No chaff services currently installed.${NC}"
            fi
            ;;
        *)
            echo -e "${RED}[!] Usage: hashem chaff on|off|status${NC}"
            return 1
            ;;
    esac
}

menu_chaff() {
    echo -e "\n${YELLOW}=== Traffic Chaff / Obfuscation (Idle-Gap Filler) ===${NC}"
    echo -e "Random pings fill idle gaps to break timing analysis (low overhead, ~few KB/s)."
    cli_chaff status
    echo ""
    echo "  1) Enable / Start chaff services (on)"
    echo "  2) Disable / Stop chaff services (off)"
    echo "  3) Check status"
    echo "  0) Back to main menu"
    echo ""
    read -p "Select an action [0-3]: " CH_OPT
    case "$CH_OPT" in
        1) cli_chaff on ;;
        2) cli_chaff off ;;
        3) cli_chaff status ;;
        0) return 0 ;;
        *) echo -e "${RED}[!] Invalid option.${NC}"; return 1 ;;
    esac
}

# ---- DPI Shield: protect reverse proxy ports against scanner floods ----
DPI_PORTS_FILE="/etc/gre-panel/dpi-ports.conf"

dpi_collect_reverse_ports() {
    local PORTS=()
    local EXCLUDE_PORTS=()

    # 1. Collect FRP bind/control ports to exclude
    local f
    for f in "${CONFIG_DIR}"/frps*.toml /etc/frp/frps*.toml; do
        [[ -f "$f" ]] || continue
        while read -r bp; do
            [[ -n "$bp" ]] && EXCLUDE_PORTS+=("$bp")
        done < <(grep -E '^\s*bindPort\s*=' "$f" 2>/dev/null | awk -F= '{print $2}' | tr -d ' "' | tr -d " \t\r\n")
    done
    for f in "${CONFIG_DIR}/frpc.toml" /etc/frp/frpc.toml; do
        [[ -f "$f" ]] || continue
        while read -r sp; do
            [[ -n "$sp" ]] && EXCLUDE_PORTS+=("$sp")
        done < <(grep -E '^\s*serverPort\s*=' "$f" 2>/dev/null | awk -F= '{print $2}' | tr -d ' "' | tr -d " \t\r\n")
    done
    if [[ -f "$PEERS_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        while read -r fp; do
            [[ -n "$fp" ]] && EXCLUDE_PORTS+=("$fp")
        done < <(PEERS_F="$PEERS_FILE" python3 -c '
import json, os
try:
    with open(os.environ["PEERS_F"]) as f:
        d = json.load(f)
        for p in d.get("peers", []):
            pt = p.get("frp_port")
            if pt:
                print(pt)
except Exception:
    pass
' 2>/dev/null)
    fi

    # 2. Collect panel ports to exclude
    if [[ -f /etc/gre-panel/panel.json ]]; then
        while read -r pp; do
            [[ -n "$pp" ]] && EXCLUDE_PORTS+=("$pp")
        done < <(grep -oE '"(port|tls_port)":\s*[0-9]+' /etc/gre-panel/panel.json 2>/dev/null | grep -oE '[0-9]+')
    fi
    EXCLUDE_PORTS+=(7777 7443)

    # 3. Collect SSH ports to exclude
    EXCLUDE_PORTS+=(22)
    if command -v ss >/dev/null 2>&1; then
        while read -r sp; do
            [[ -n "$sp" ]] && EXCLUDE_PORTS+=("$sp")
        done < <(ss -ltnp 2>/dev/null | grep 'sshd' | awk '{print $4}' | awk -F: '{print $NF}')
    fi

    # Candidate reverse ports:
    # A. peers.json
    if [[ -f "$PEERS_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        while read -r p; do
            [[ -n "$p" ]] && PORTS+=("$p")
        done < <(PEERS_F="$PEERS_FILE" python3 -c '
import json, os
try:
    with open(os.environ["PEERS_F"]) as f:
        d = json.load(f)
        for p in d.get("peers", []):
            for pt in p.get("ports", []):
                print(pt)
except Exception:
    pass
' 2>/dev/null)
    fi

    # B. frpc.toml remotePort
    for f in "${CONFIG_DIR}/frpc.toml" /etc/frp/frpc.toml; do
        [[ -f "$f" ]] || continue
        while read -r p; do
            [[ -n "$p" ]] && PORTS+=("$p")
        done < <(grep -E '^\s*remotePort\s*=' "$f" 2>/dev/null | awk -F= '{print $2}' | tr -d ' "' | tr -d " \t\r\n")
    done

    # C. Active frps listeners via ss -ltn (excluding control ports)
    if command -v ss >/dev/null 2>&1; then
        while read -r p; do
            [[ -n "$p" ]] && PORTS+=("$p")
        done < <(ss -ltnp 2>/dev/null | grep -E 'users:.*\("frps"' | awk '{print $4}' | awk -F: '{print $NF}')
    fi

    # D. Saved DPI ports cache (for reboots before frps connects)
    if [[ -f "$DPI_PORTS_FILE" ]]; then
        while read -r p; do
            [[ -n "$p" ]] && PORTS+=("$p")
        done < "$DPI_PORTS_FILE"
    fi

    # Filter candidates: remove excluded, check validity (1..65535)
    local FINAL_PORTS=()
    local p ex excluded
    for p in "${PORTS[@]}"; do
        [[ "$p" =~ ^[0-9]+$ ]] || continue
        (( p >= 1 && p <= 65535 )) || continue
        excluded=0
        for ex in "${EXCLUDE_PORTS[@]}"; do
            if [[ "$p" -eq "$ex" ]]; then
                excluded=1
                break
            fi
        done
        [[ "$excluded" -eq 0 ]] && FINAL_PORTS+=("$p")
    done

    if [[ ${#FINAL_PORTS[@]} -gt 0 ]]; then
        printf "%s\n" "${FINAL_PORTS[@]}" | sort -n -u
    fi
}

dpi_shield_on() {
    command -v iptables >/dev/null 2>&1 || {
        echo -e "${RED}[!] iptables is required for DPI shield but not installed.${NC}"
        return 1
    }

    local REVERSE_PORTS=()
    while read -r p; do
        [[ -n "$p" ]] && REVERSE_PORTS+=("$p")
    done < <(dpi_collect_reverse_ports)

    if [[ ${#REVERSE_PORTS[@]} -eq 0 ]]; then
        echo -e "${YELLOW}[!] No reverse tunnel ports found in peers.json, frpc.toml, or active frps listeners.${NC}"
        echo -e "${YELLOW}[*] Set up a tunnel or configure reverse ports first.${NC}"
        return 1
    fi

    mkdir -p "$(dirname "$DPI_PORTS_FILE")"
    printf "%s\n" "${REVERSE_PORTS[@]}" > "$DPI_PORTS_FILE"

    echo -e "${CYAN}[*] Installing DPI shield for reverse ports: ${REVERSE_PORTS[*]}...${NC}"

    # Idempotent chain setup: flush existing HASHEM-DPI chain or create it
    if iptables -L HASHEM-DPI -n >/dev/null 2>&1; then
        iptables -F HASHEM-DPI
    else
        iptables -N HASHEM-DPI
    fi

    # Ensure jump from INPUT exists
    if ! iptables -C INPUT -j HASHEM-DPI 2>/dev/null; then
        iptables -I INPUT 1 -j HASHEM-DPI
    fi

    # Add per-port hashlimit rules
    local port
    local DPI_RATE=$(perf_get_dpi_rate)
    local DPI_BURST=$(perf_get_dpi_burst)
    for port in "${REVERSE_PORTS[@]}"; do
        iptables -A HASHEM-DPI -p tcp --dport "$port" -m limit --limit "$DPI_RATE" --limit-burst "$DPI_BURST" -j ACCEPT
        iptables -A HASHEM-DPI -p tcp --dport "$port" -j DROP
    done

    # Persist across reboot via systemd oneshot unit
    [[ -x "$HASHEM_BIN" ]] || { cp "$0" "$HASHEM_BIN" 2>/dev/null && chmod +x "$HASHEM_BIN"; } || true
    cat << 'EOF' > /etc/systemd/system/hashem-dpi.service
[Unit]
Description=Hashem DPI Shield Protection
DefaultDependencies=no
After=systemd-modules-load.service local-fs.target
Before=network-pre.target
Wants=network-pre.target

[Service]
Type=oneshot
RemainAfterExit=yes
ExecStart=/usr/local/bin/hashem dpi-shield on

[Install]
WantedBy=network-pre.target
EOF
    systemctl daemon-reload
    systemctl enable hashem-dpi.service >/dev/null 2>&1 || true

    echo -e "${GREEN}[✔️] DPI shield ACTIVE: ${#REVERSE_PORTS[@]} port(s) protected (${REVERSE_PORTS[*]}).${NC}"
    echo -e "${GREEN}[✔️] Persisted via systemd unit hashem-dpi.service (WantedBy=network-pre.target).${NC}"
}

dpi_shield_off() {
    # Disable and remove systemd persistence unit
    systemctl disable --now hashem-dpi.service >/dev/null 2>&1 || true
    rm -f /etc/systemd/system/hashem-dpi.service "$DPI_PORTS_FILE"
    systemctl daemon-reload

    # Remove jump from INPUT
    while iptables -C INPUT -j HASHEM-DPI 2>/dev/null; do
        iptables -D INPUT -j HASHEM-DPI
    done

    # Flush and delete HASHEM-DPI chain
    iptables -F HASHEM-DPI 2>/dev/null || true
    iptables -X HASHEM-DPI 2>/dev/null || true

    echo -e "${GREEN}[✔️] DPI shield DISABLED (HASHEM-DPI chain removed and service disabled).${NC}"
}

dpi_shield_status() {
    if iptables -L HASHEM-DPI -n >/dev/null 2>&1; then
        echo -e "${GREEN}[✔️] DPI shield is ACTIVE (chain HASHEM-DPI installed).${NC}"
        echo -e "${CYAN}Packet counters and rules in HASHEM-DPI:${NC}"
        iptables -L HASHEM-DPI -v -n
        if systemctl is-enabled hashem-dpi.service >/dev/null 2>&1; then
            echo -e "${GREEN}[✔️] Persistence: hashem-dpi.service is enabled.${NC}"
        else
            echo -e "${YELLOW}[!] Persistence: hashem-dpi.service is not enabled.${NC}"
        fi
    else
        echo -e "${YELLOW}[!] DPI shield is INACTIVE (chain HASHEM-DPI does not exist).${NC}"
        if systemctl is-enabled hashem-dpi.service >/dev/null 2>&1; then
            echo -e "${YELLOW}[*] hashem-dpi.service is enabled for boot.${NC}"
        fi
    fi
}

cli_dpi_shield() {
    local ACTION="${1:-}"
    case "$ACTION" in
        on)
            dpi_shield_on
            ;;
        off)
            dpi_shield_off
            ;;
        status)
            dpi_shield_status
            ;;
        *)
            echo -e "${RED}[!] Usage: hashem dpi-shield on|off|status${NC}"
            return 1
            ;;
    esac
}

menu_dpi_shield() {
    echo -e "\n${YELLOW}=== DPI Shield (Reverse Port Flood Protection) ===${NC}"
    echo -e "Protects reverse ports against DPI scanner floods using iptables rate limiting."
    echo ""
    cli_dpi_shield status
    echo ""
    echo "  1) Enable DPI Shield (on)"
    echo "  2) Disable DPI Shield (off)"
    echo "  3) Check status"
    echo "  0) Back to main menu"
    echo ""
    read -p "Select an action [0-3]: " DPI_OPT
    case "$DPI_OPT" in
        1) cli_dpi_shield on ;;
        2) cli_dpi_shield off ;;
        3) cli_dpi_shield status ;;
        0) return 0 ;;
        *) echo -e "${RED}[!] Invalid option.${NC}"; return 1 ;;
    esac
}

# ---- Performance & Obfuscation Controls (CLI + Menu 22) ----

perf_apply() {
    init_perf_json
    local EFF_ENC=$(perf_get_enc)
    local EFF_COMP=$(perf_get_comp)
    local EFF_TLS=$(perf_get_tls)

    local IS_FOREIGN=0
    local IS_IRAN=0
    [[ -f "${CONFIG_DIR}/frpc.toml" ]] && IS_FOREIGN=1
    [[ -f "${CONFIG_DIR}/frps.toml" ]] && IS_IRAN=1
    for f in "${CONFIG_DIR}"/frps*.toml; do
        [[ -f "$f" ]] && IS_IRAN=1
    done

    if [[ "$IS_FOREIGN" -eq 0 && "$IS_IRAN" -eq 0 ]]; then
        echo -e "${YELLOW}[!] No frps.toml or frpc.toml found in ${CONFIG_DIR}.${NC}"
        echo -e "${YELLOW}[*] Set up a tunnel first before applying performance settings.${NC}"
        return 1
    fi

    echo -e "${CYAN}[*] Applying performance settings (enc=${EFF_ENC} comp=${EFF_COMP} tls=${EFF_TLS})...${NC}"

    if [[ "$IS_FOREIGN" -eq 1 ]]; then
        local TOML_FILE="${CONFIG_DIR}/frpc.toml"
        if command -v python3 >/dev/null 2>&1; then
            python3 -c '
path = "'"$TOML_FILE"'"
enc = bool('"$EFF_ENC"')
comp = bool('"$EFF_COMP"')
tls = bool('"$EFF_TLS"')

with open(path, "r") as f:
    lines = f.read().splitlines()

sections = []
current = []
for line in lines:
    if line.strip().startswith("[[proxies]]"):
        if current:
            sections.append(current)
        current = [line]
    else:
        current.append(line)
if current:
    sections.append(current)

out_sections = []
for i, sec in enumerate(sections):
    if i == 0 and not sec[0].strip().startswith("[[proxies]]"):
        new_sec = []
        has_tls_enable = False
        for l in sec:
            s = l.strip()
            if s.startswith("transport.tls.disableCustomTLSFirstByte"):
                continue
            if s.startswith("transport.tls.enable"):
                has_tls_enable = True
            new_sec.append(l)
        final_hdr = []
        for l in new_sec:
            final_hdr.append(l)
            if l.strip().startswith("transport.tls.enable") and tls:
                final_hdr.append("transport.tls.disableCustomTLSFirstByte = true")
        if tls and not any("transport.tls.disableCustomTLSFirstByte" in x for x in final_hdr):
            if not has_tls_enable:
                final_hdr.append("transport.tls.enable = true")
            final_hdr.append("transport.tls.disableCustomTLSFirstByte = true")
        out_sections.append(final_hdr)
    else:
        new_sec = []
        for l in sec:
            s = l.strip()
            if s.startswith("transport.useEncryption") or s.startswith("transport.useCompression"):
                continue
            new_sec.append(l)
        while new_sec and new_sec[-1].strip() == "":
            new_sec.pop()
        if enc:
            new_sec.append("transport.useEncryption = true")
        if comp:
            new_sec.append("transport.useCompression = true")
        new_sec.append("")
        out_sections.append(new_sec)

result = "\n".join("\n".join(s) for s in out_sections).strip() + "\n"
with open(path, "w") as f:
    f.write(result)
'
        fi
        systemctl daemon-reload >/dev/null 2>&1 || true
        systemctl restart frpc
        echo -e "${GREEN}[✔️] frpc.toml updated & frpc service restarted.${NC}"
    fi

    if [[ "$IS_IRAN" -eq 1 ]]; then
        for TOML_FILE in "${CONFIG_DIR}"/frps*.toml; do
            [[ -f "$TOML_FILE" ]] || continue
            if command -v python3 >/dev/null 2>&1; then
                python3 -c '
path = "'"$TOML_FILE"'"
tls = bool('"$EFF_TLS"')

with open(path, "r") as f:
    lines = f.read().splitlines()

new_lines = []
for l in lines:
    s = l.strip()
    if s.startswith("transport.tls.force"):
        continue
    new_lines.append(l)

final_lines = []
has_tls = False
for l in new_lines:
    final_lines.append(l)
    if l.strip().startswith("auth.token") and tls:
        final_lines.append("transport.tls.force = true")
        has_tls = True

if tls and not has_tls:
    final_lines.append("transport.tls.force = true")

result = "\n".join(final_lines).strip() + "\n"
with open(path, "w") as f:
    f.write(result)
'
            fi
        done
        systemctl daemon-reload >/dev/null 2>&1 || true
        systemctl restart frps >/dev/null 2>&1 || true
        for s in /etc/systemd/system/frps-*.service; do
            [[ -f "$s" ]] || continue
            local sname=$(basename "$s")
            systemctl restart "$sname" >/dev/null 2>&1 || true
        done
        echo -e "${GREEN}[✔️] frps toml(s) updated & frps service(s) restarted.${NC}"
    fi

    # Also apply chaff profile
    local CHAFF_PROF=$(perf_get_chaff)
    if [[ "$CHAFF_PROF" == "off" ]]; then
        cli_chaff off >/dev/null 2>&1 || true
    else
        CHAFF_PROFILE="$CHAFF_PROF" cli_chaff on >/dev/null 2>&1 || true
    fi

    # Also apply DPI shield setting
    local DPI_EN=$(perf_get_dpi_enabled)
    if [[ "$DPI_EN" == "1" ]]; then
        dpi_shield_on >/dev/null 2>&1 || true
    else
        dpi_shield_off >/dev/null 2>&1 || true
    fi

    echo -e "${GREEN}[✔️] Performance settings successfully applied.${NC}"
    return 0
}

cli_perf() {
    local SUB="${1:-status}"
    case "$SUB" in
        status)
            init_perf_json
            local ENC=$(perf_get_enc)
            local COMP=$(perf_get_comp)
            local TLS=$(perf_get_tls)
            local CHAFF=$(perf_get_chaff)
            local DPI_EN=$(perf_get_dpi_enabled)
            local DPI_R=$(perf_get_dpi_rate)
            local DPI_B=$(perf_get_dpi_burst)

            echo -e "\n${CYAN}==========================================================${NC}"
            echo -e "${CYAN}            Performance & Obfuscation Status              ${NC}"
            echo -e "${CYAN}==========================================================${NC}"
            echo -e "Settings (/etc/gre-panel/perf.json):"
            echo -e "  Proxy Encryption:  $([[ "$ENC" == "1" ]] && echo -e "${GREEN}on${NC}" || echo -e "${YELLOW}off${NC}")"
            echo -e "  Proxy Compression: $([[ "$COMP" == "1" ]] && echo -e "${GREEN}on${NC}" || echo -e "${YELLOW}off${NC}")"
            echo -e "  Forced TLS:        $([[ "$TLS" == "1" ]] && echo -e "${GREEN}on${NC}" || echo -e "${YELLOW}off${NC}")"
            echo -e "  Chaff Profile:     ${CYAN}${CHAFF}${NC}"
            echo -e "  DPI Shield:        $([[ "$DPI_EN" == "1" ]] && echo -e "${GREEN}enabled${NC} (${DPI_R}, burst ${DPI_B})" || echo -e "${YELLOW}disabled${NC}")"

            if [[ -n "${PERF_ENC:-}" || -n "${PERF_COMP:-}" || -n "${PERF_TLS:-}" ]]; then
                echo -e "${YELLOW}[!] Env overrides active: PERF_ENC=${PERF_ENC:-unset} PERF_COMP=${PERF_COMP:-unset} PERF_TLS=${PERF_TLS:-unset}${NC}"
            fi

            echo ""
            echo -e "Live Tunnel Configuration:"
            local MATCH=1

            if [[ -f "${CONFIG_DIR}/frpc.toml" ]]; then
                local LIVE_ENC=0 LIVE_COMP=0 LIVE_TLS=0
                grep -E -q '^[[:space:]]*transport\.useEncryption[[:space:]]*=[[:space:]]*true' "${CONFIG_DIR}/frpc.toml" && LIVE_ENC=1
                grep -E -q '^[[:space:]]*transport\.useCompression[[:space:]]*=[[:space:]]*true' "${CONFIG_DIR}/frpc.toml" && LIVE_COMP=1
                grep -E -q '^[[:space:]]*transport\.tls\.disableCustomTLSFirstByte[[:space:]]*=[[:space:]]*true' "${CONFIG_DIR}/frpc.toml" && LIVE_TLS=1

                echo -e "  Role: Foreign client (frpc)"
                echo -e "  Live Proxy Encryption:  $([[ "$LIVE_ENC" == "1" ]] && echo "on" || echo "off") $([[ "$LIVE_ENC" == "$ENC" ]] && echo -e "${GREEN}[MATCH]${NC}" || { echo -e "${RED}[MISMATCH]${NC}"; MATCH=0; })"
                echo -e "  Live Proxy Compression: $([[ "$LIVE_COMP" == "1" ]] && echo "on" || echo "off") $([[ "$LIVE_COMP" == "$COMP" ]] && echo -e "${GREEN}[MATCH]${NC}" || { echo -e "${RED}[MISMATCH]${NC}"; MATCH=0; })"
                echo -e "  Live Forced TLS:        $([[ "$LIVE_TLS" == "1" ]] && echo "on" || echo "off") $([[ "$LIVE_TLS" == "$TLS" ]] && echo -e "${GREEN}[MATCH]${NC}" || { echo -e "${RED}[MISMATCH]${NC}"; MATCH=0; })"
            elif [[ -f "${CONFIG_DIR}/frps.toml" ]] || ls "${CONFIG_DIR}"/frps*.toml >/dev/null 2>&1; then
                local LIVE_TLS=0
                local F
                for F in "${CONFIG_DIR}"/frps*.toml; do
                    [[ -f "$F" ]] || continue
                    grep -E -q '^[[:space:]]*transport\.tls\.force[[:space:]]*=[[:space:]]*true' "$F" && LIVE_TLS=1
                done
                echo -e "  Role: Iran server (frps)"
                echo -e "  Live Forced TLS:        $([[ "$LIVE_TLS" == "1" ]] && echo "on" || echo "off") $([[ "$LIVE_TLS" == "$TLS" ]] && echo -e "${GREEN}[MATCH]${NC}" || { echo -e "${RED}[MISMATCH]${NC}"; MATCH=0; })"
                echo -e "  (Proxy encryption & compression are client-side settings on Foreign VPS)"
            else
                echo -e "  No live tunnel configs found."
            fi

            # DPI live
            if iptables -L HASHEM-DPI -n >/dev/null 2>&1; then
                echo -e "  DPI Shield (iptables):  ${GREEN}ACTIVE${NC}"
            else
                echo -e "  DPI Shield (iptables):  ${YELLOW}INACTIVE${NC}"
            fi

            # Chaff live
            if systemctl is-active --quiet gre-chaff 2>/dev/null || systemctl list-units --type=service 2>/dev/null | grep -q 'gre-chaff.*running'; then
                echo -e "  Chaff Service:          ${GREEN}RUNNING${NC}"
            else
                echo -e "  Chaff Service:          ${YELLOW}STOPPED${NC}"
            fi

            echo ""
            if [[ "$MATCH" -eq 1 ]]; then
                echo -e "${GREEN}[✔️] Live configuration matches effective settings.${NC}"
            else
                echo -e "${RED}[!] Live configuration does NOT match settings. Run 'hashem perf apply' to sync.${NC}"
            fi
            ;;
        enc)
            local VAL="${2:-}"
            case "$VAL" in
                on)  perf_set_val "proxy_encryption" "true" 1; echo -e "${GREEN}[✔️] Proxy encryption set to 'on'. Run 'hashem perf apply' to apply and restart tunnels.${NC}" ;;
                off) perf_set_val "proxy_encryption" "false" 1; echo -e "${GREEN}[✔️] Proxy encryption set to 'off'. Run 'hashem perf apply' to apply and restart tunnels.${NC}" ;;
                *)   echo -e "${RED}[!] Usage: hashem perf enc on|off${NC}"; return 1 ;;
            esac
            ;;
        comp)
            local VAL="${2:-}"
            case "$VAL" in
                on)  perf_set_val "proxy_compression" "true" 1; echo -e "${GREEN}[✔️] Proxy compression set to 'on'. Run 'hashem perf apply' to apply and restart tunnels.${NC}" ;;
                off) perf_set_val "proxy_compression" "false" 1; echo -e "${GREEN}[✔️] Proxy compression set to 'off'. Run 'hashem perf apply' to apply and restart tunnels.${NC}" ;;
                *)   echo -e "${RED}[!] Usage: hashem perf comp on|off${NC}"; return 1 ;;
            esac
            ;;
        tls)
            local VAL="${2:-}"
            case "$VAL" in
                on)  perf_set_val "force_tls" "true" 1; echo -e "${GREEN}[✔️] Forced TLS set to 'on'. Run 'hashem perf apply' to apply and restart tunnels.${NC}" ;;
                off) perf_set_val "force_tls" "false" 1; echo -e "${GREEN}[✔️] Forced TLS set to 'off'. Run 'hashem perf apply' to apply and restart tunnels.${NC}" ;;
                *)   echo -e "${RED}[!] Usage: hashem perf tls on|off${NC}"; return 1 ;;
            esac
            ;;
        chaff)
            local VAL="${2:-}"
            case "$VAL" in
                off)
                    perf_set_val "chaff_profile" "off" 0
                    cli_chaff off
                    echo -e "${GREEN}[✔️] Chaff profile set to 'off' and services stopped.${NC}"
                    ;;
                low|mid)
                    perf_set_val "chaff_profile" "$VAL" 0
                    CHAFF_PROFILE="$VAL" cli_chaff on
                    echo -e "${GREEN}[✔️] Chaff profile set to '$VAL' and services started.${NC}"
                    ;;
                *)
                    echo -e "${RED}[!] Usage: hashem perf chaff off|low|mid${NC}"
                    return 1
                    ;;
            esac
            ;;
        dpi)
            local VAL="${2:-}"
            case "$VAL" in
                on)
                    perf_set_val "dpi_enabled" "true" 1
                    dpi_shield_on
                    echo -e "${GREEN}[✔️] DPI shield enabled.${NC}"
                    ;;
                off)
                    perf_set_val "dpi_enabled" "false" 1
                    dpi_shield_off
                    echo -e "${GREEN}[✔️] DPI shield disabled.${NC}"
                    ;;
                *)
                    echo -e "${RED}[!] Usage: hashem perf dpi on|off${NC}"
                    return 1
                    ;;
            esac
            ;;
        apply)
            perf_apply
            ;;
        -h|--help|help)
            echo "Usage: hashem perf status|enc on|off|comp on|off|tls on|off|chaff off|low|mid|dpi on|off|apply"
            ;;
        *)
            echo -e "${RED}[!] Unknown subcommand: $SUB${NC}"
            echo "Usage: hashem perf status|enc on|off|comp on|off|tls on|off|chaff off|low|mid|dpi on|off|apply"
            return 1
            ;;
    esac
}

menu_perf() {
    while true; do
        cli_perf status
        echo ""
        echo "  1) Toggle Proxy Encryption (enc on/off)"
        echo "  2) Toggle Proxy Compression (comp on/off)"
        echo "  3) Toggle Forced TLS (tls on/off)"
        echo "  4) Set Chaff Profile (off / low / mid)"
        echo "  5) Toggle DPI Shield (on/off)"
        echo "  6) Apply settings & restart tunnels"
        echo "  0) Back to main menu"
        echo ""
        read -p "Select an option [0-6]: " P_OPT
        case "$P_OPT" in
            1)
                local cur=$(perf_get_enc)
                if [[ "$cur" == "1" ]]; then cli_perf enc off; else cli_perf enc on; fi
                ;;
            2)
                local cur=$(perf_get_comp)
                if [[ "$cur" == "1" ]]; then cli_perf comp off; else cli_perf comp on; fi
                ;;
            3)
                local cur=$(perf_get_tls)
                if [[ "$cur" == "1" ]]; then cli_perf tls off; else cli_perf tls on; fi
                ;;
            4)
                echo "Select chaff profile:"
                echo "  1) off"
                echo "  2) low (default)"
                echo "  3) mid"
                read -p "Option [1-3]: " C_OPT
                case "$C_OPT" in
                    1) cli_perf chaff off ;;
                    2) cli_perf chaff low ;;
                    3) cli_perf chaff mid ;;
                    *) echo "Invalid option." ;;
                esac
                ;;
            5)
                local cur=$(perf_get_dpi_enabled)
                if [[ "$cur" == "1" ]]; then cli_perf dpi off; else cli_perf dpi on; fi
                ;;
            6)
                cli_perf apply
                ;;
            0)
                return 0
                ;;
            *)
                echo -e "${RED}[!] Invalid option.${NC}"
                ;;
        esac
    done
}


# ---- SINGLE SOURCE OF TRUTH for install logic ----
# setup_iran_server_noninteractive / setup_foreign_server_noninteractive do the
# real work. The interactive menu functions below only prompt + validate, then
# delegate here. The web panel calls the same functions via the CLI flags at
# the bottom of this file (setup-iran / setup-foreign), so all three paths
# (menu, CLI, panel) execute identical steps.
# Args: $1=local_pub $2=remote_pub $3=frp_port $4=token [$5=local_gre [$6=peer_gre [$7="cleaned ports"]]]
setup_iran_server_noninteractive() {
    local IP_IRAN=$1 IP_FOREIGN=$2 BIND_PORT=$3 TOKEN=$4
    local LOCAL_GRE=${5:-$IRAN_GRE_IP} PEER_GRE=${6:-$FOREIGN_GRE_IP}
    setup_gre_systemd "$IP_IRAN" "$IP_FOREIGN" "$LOCAL_GRE"
    install_frp_binaries
    local EFF_TLS=$(perf_get_tls)
    local TLS_LINE=""
    [[ "$EFF_TLS" == "1" ]] && TLS_LINE="transport.tls.force = true"
    cat <<EOF > "${CONFIG_DIR}/frps.toml"
bindAddr = "0.0.0.0"
bindPort = ${BIND_PORT}
auth.method = "token"
auth.token = "${TOKEN}"
${TLS_LINE:+$TLS_LINE
}transport.tcpMux = true
transport.maxPoolCount = 200
EOF
    cat <<EOF > /etc/systemd/system/frps.service
[Unit]
Description=FRP Server Service
After=network.target ${TUNNEL_NAME}.service
Wants=${TUNNEL_NAME}.service

[Service]
Type=simple
User=root
Restart=always
RestartSec=5s
ExecStart=${INSTALL_DIR}/frps -c ${CONFIG_DIR}/frps.toml

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable frps >/dev/null 2>&1
    systemctl restart frps
    setup_chaff "" "$PEER_GRE"
    if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
        ufw allow "${BIND_PORT}/tcp" >/dev/null 2>&1
    fi
    echo -e "${GREEN}[✔️] IRAN setup done: GRE ${IP_IRAN} <-> ${IP_FOREIGN} (${LOCAL_GRE} peer ${PEER_GRE}), frps :${BIND_PORT}${NC}"
    echo -e "${YELLOW}Token: ${TOKEN} (copy to the FOREIGN side)${NC}"
    echo -e "BUNDLE:$(bundle_make "$IP_IRAN" "$BIND_PORT" "$LOCAL_GRE" "$PEER_GRE" "$TOKEN")"
    local DPI_EN=$(perf_get_dpi_enabled)
    if [[ "$DPI_EN" != "0" ]]; then
        dpi_shield_on >/dev/null 2>&1 || true
    fi
    if [[ "${GRE_SKIP_PANEL:-0}" == "1" ]]; then
        echo -e "${CYAN}[*] Skipping panel install (called from panel).${NC}"
    else
        install_panel || echo -e "${YELLOW}[!] Panel auto-install failed — retry from menu option 15 (Update All).${NC}"
    fi
    if [[ "${GRE_SKIP_PANEL:-0}" != "1" && -t 0 ]]; then
        echo ""
        echo -e "${CYAN}--- Panel HTTPS (optional but recommended) ---${NC}"
        echo -e "The panel currently runs on plain HTTP. If this server has a domain"
        echo -e "pointing to it, you can get a free Let's Encrypt certificate now:"
        read -p "Get HTTPS certificate for the panel now? [y/N]: " TLS_WANT
        if [[ "$TLS_WANT" =~ ^[Yy]$ ]]; then
            panel_tls_issue || echo -e "${YELLOW}[!] TLS skipped — panel still works on HTTP; retry from menu option 16.${NC}"
        else
            echo -e "${CYAN}[*] Skipped — enable later from menu option 16 or web Settings → HTTPS certificate.${NC}"
        fi
    fi
}

setup_foreign_server_noninteractive() {
    local IP_FOREIGN=$1 IP_IRAN=$2 SERVER_PORT=$3 TOKEN=$4
    local LOCAL_GRE=${5:-$FOREIGN_GRE_IP} PEER_GRE=${6:-$IRAN_GRE_IP}
    local PORTS_CLEANED=${7:-}
    _setup_foreign_full "$IP_FOREIGN" "$IP_IRAN" "$SERVER_PORT" "$TOKEN" "$LOCAL_GRE" "$PEER_GRE" "$PORTS_CLEANED"
}

# shared full foreign path: GRE + ping feedback + frpc binaries/config/service + panel.
# Called by the interactive menu, the CLI, and (via CLI) the web panel.
_setup_foreign_full() {
    local IP_FOREIGN=$1 IP_IRAN=$2 SERVER_PORT=$3 TOKEN=$4
    local LOCAL_GRE=$5 PEER_GRE=$6 PORTS_CLEANED=$7
    setup_gre_systemd "$IP_FOREIGN" "$IP_IRAN" "$LOCAL_GRE"
    echo -e "${CYAN}[*] Testing GRE internal ping to Iran (${PEER_GRE})...${NC}"
    if ping -c 3 -W 2 "$PEER_GRE" >/dev/null 2>&1; then
        echo -e "${GREEN}[✔️] GRE Tunnel link is UP and reachable!${NC}"
    else
        echo -e "${YELLOW}[!] Warning: Ping to ${PEER_GRE} did not respond yet.${NC}"
    fi
    install_frp_binaries
    local EFF_TLS=$(perf_get_tls)
    local EFF_ENC=$(perf_get_enc)
    local EFF_COMP=$(perf_get_comp)
    local TLS_CUSTOM=""
    [[ "$EFF_TLS" == "1" ]] && TLS_CUSTOM="transport.tls.disableCustomTLSFirstByte = true"
    cat <<EOF > "${CONFIG_DIR}/frpc.toml"
serverAddr = "${PEER_GRE}"
serverPort = ${SERVER_PORT}
auth.method = "token"
auth.token = "${TOKEN}"
transport.tls.enable = true
${TLS_CUSTOM:+$TLS_CUSTOM
}transport.poolCount = 25

EOF
    local PORT
    local ENC_LINE=""
    [[ "$EFF_ENC" == "1" ]] && ENC_LINE="transport.useEncryption = true"
    local COMP_LINE=""
    [[ "$EFF_COMP" == "1" ]] && COMP_LINE="transport.useCompression = true"
    for PORT in $PORTS_CLEANED; do
        cat <<EOF >> "${CONFIG_DIR}/frpc.toml"
[[proxies]]
name = "tcp_${PORT}"
type = "tcp"
localIP = "127.0.0.1"
localPort = ${PORT}
remotePort = ${PORT}
${ENC_LINE:+$ENC_LINE
}${COMP_LINE:+$COMP_LINE
}
[[proxies]]
name = "udp_${PORT}"
type = "udp"
localIP = "127.0.0.1"
localPort = ${PORT}
remotePort = ${PORT}
${ENC_LINE:+$ENC_LINE
}${COMP_LINE:+$COMP_LINE
}
EOF
    done
    cat <<EOF > /etc/systemd/system/frpc.service
[Unit]
Description=FRP Client Reverse Service
After=network.target ${TUNNEL_NAME}.service
Wants=${TUNNEL_NAME}.service

[Service]
Type=simple
User=root
Restart=always
RestartSec=5s
ExecStart=${INSTALL_DIR}/frpc -c ${CONFIG_DIR}/frpc.toml

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable frpc >/dev/null 2>&1
    systemctl restart frpc
    setup_chaff "" "$PEER_GRE"
    echo -e "${GREEN}[✔️] FOREIGN setup done: GRE ${IP_FOREIGN} <-> ${IP_IRAN} (${LOCAL_GRE} peer ${PEER_GRE}), frpc → ${PEER_GRE}:${SERVER_PORT}${NC}"
    echo -e "${GREEN}Reverse ports: ${PORTS_CLEANED} (TCP & UDP, TLS)${NC}"
    local DPI_EN=$(perf_get_dpi_enabled)
    if [[ "$DPI_EN" != "0" ]]; then
        dpi_shield_on >/dev/null 2>&1 || true
    fi
    if [[ "${GRE_SKIP_PANEL:-0}" == "1" ]]; then
        echo -e "${CYAN}[*] Skipping panel install (called from panel).${NC}"
    else
        install_panel || echo -e "${YELLOW}[!] Panel auto-install failed — retry from menu option 15 (Update All).${NC}"
    fi
    if [[ "${GRE_SKIP_PANEL:-0}" != "1" && -t 0 ]]; then
        echo ""
        echo -e "${CYAN}--- Panel HTTPS (optional but recommended) ---${NC}"
        echo -e "The panel currently runs on plain HTTP. If this server has a domain"
        echo -e "pointing to it, you can get a free Let's Encrypt certificate now:"
        read -p "Get HTTPS certificate for the panel now? [y/N]: " TLS_WANT_F
        if [[ "$TLS_WANT_F" =~ ^[Yy]$ ]]; then
            panel_tls_issue || echo -e "${YELLOW}[!] TLS skipped — panel still works on HTTP; retry from menu option 16.${NC}"
        else
            echo -e "${CYAN}[*] Skipped — enable later from menu option 16 or web Settings → HTTPS certificate.${NC}"
        fi
    fi
}

# ---- Multi-peer tunnels: up to MAX_PEERS foreign servers on one Iran ----
# Peer 1 reuses the legacy names (gre-tunnel, frps.toml, frps.service) so
# existing installs keep working. Peers 2..5 get gre-tN + frps-N.toml +
# frps-N.service, each with its own token and control port (one frps
# understands only one token). Registry: /etc/gre-panel/peers.json.
PEERS_FILE="/etc/gre-panel/peers.json"
MAX_PEERS=5

peer_init() {
    mkdir -p "$(dirname "$PEERS_FILE")" "$CONFIG_DIR"
    [[ -f "$PEERS_FILE" ]] || echo '{"peers":[]}' > "$PEERS_FILE"
}

peer_require_py() {
    command -v python3 >/dev/null 2>&1 || { echo -e "${RED}[!] python3 is required for peer management.${NC}"; return 1; }
}

# print registry as-is (JSON)
peer_list() { peer_init; cat "$PEERS_FILE"; }

# smallest free peer id (1..MAX_PEERS), or 0 when full
peer_next_id() {
    peer_require_py || return 1
    PEERS_F="$PEERS_FILE" MAX_PEERS="$MAX_PEERS" python3 -c \
'import json,os; d=json.load(open(os.environ["PEERS_F"])); used={p["id"] for p in d.get("peers",[])}; ids=[i for i in range(1,int(os.environ["MAX_PEERS"])+1) if i not in used]; print(ids[0] if ids else 0)'
}

# space-separated "port:peername" of all claimed reverse ports
peer_ports_used() {
    peer_init; peer_require_py || return 1
    PEERS_F="$PEERS_FILE" python3 -c \
'import json,os; d=json.load(open(os.environ["PEERS_F"])); print(" ".join(str(p) + ":" + str(r.get("name","")) for r in d.get("peers",[]) for p in r.get("ports",[])))'
}

# $1=id -> compact JSON record or empty
peer_get() {
    PEERS_F="$PEERS_FILE" PEER_ID="$1" python3 -c \
'import json,os; d=json.load(open(os.environ["PEERS_F"])); m=[p for p in d.get("peers",[]) if p["id"]==int(os.environ["PEER_ID"])]; print(json.dumps(m[0]) if m else "")'
}

peer_token() {
    peer_init; peer_require_py || return 1
    local ID=$1 rec
    rec=$(peer_get "$ID")
    [[ -n "$rec" ]] || { echo -e "${RED}[!] No peer with id $ID.${NC}"; return 1; }
    echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])'
    # second line: full foreign-setup bundle (token + addresses + ports).
    # First-line token output stays unchanged for scripts.
    local B_TOK LIP RIP FP LGRE PGRE PTS
    B_TOK=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin)["token"])')
    LIP=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("local_pub",""))')
    RIP=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("remote_pub",""))')
    FP=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("frp_port",""))')
    LGRE=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("local_gre",""))')
    PGRE=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("peer_gre",""))')
    PTS=$(echo "$rec" | python3 -c 'import json,sys; print(" ".join(str(x) for x in json.load(sys.stdin).get("ports",[])))')
    if is_valid_ip "$LIP" && is_valid_port "$FP" && is_valid_ip "$LGRE" && is_valid_ip "$PGRE"; then
        echo "BUNDLE:$(bundle_make "$LIP" "$FP" "$LGRE" "$PGRE" "$B_TOK" "$PTS")"
    fi
}

# write one frps instance: $1=suffix("" for legacy, "-N" for peers) $2=bind_port $3=token
peer_write_frps() {
    local SUF=$1 BIND_PORT=$2 TOKEN=$3
    local EFF_TLS=$(perf_get_tls)
    local TLS_LINE=""
    [[ "$EFF_TLS" == "1" ]] && TLS_LINE="transport.tls.force = true"
    cat <<EOF > "${CONFIG_DIR}/frps${SUF}.toml"
bindAddr = "0.0.0.0"
bindPort = ${BIND_PORT}
auth.method = "token"
auth.token = "${TOKEN}"
${TLS_LINE:+$TLS_LINE
}transport.tcpMux = true
transport.maxPoolCount = 200
EOF
    local SVC="frps${SUF}"
    cat <<EOF > /etc/systemd/system/${SVC}.service
[Unit]
Description=FRP Server Service${SUF:+ (peer${SUF#-})}
After=network.target

[Service]
Type=simple
User=root
Restart=always
RestartSec=5s
ExecStart=${INSTALL_DIR}/frps -c ${CONFIG_DIR}/frps${SUF}.toml

[Install]
WantedBy=multi-user.target
EOF
    systemctl daemon-reload
    systemctl enable "$SVC" >/dev/null 2>&1
    systemctl restart "$SVC"
    if command -v ufw >/dev/null 2>&1 && ufw status | grep -q "Status: active"; then
        ufw allow "${BIND_PORT}/tcp" >/dev/null 2>&1
    fi
}

# add a peer tunnel on the Iran side.
# Flags: --name --local-pub --remote-pub --frp-port --token --local-gre --peer-gre --ports "443, 2083" [--bundle hsh1_...] [--chaff low|mid|off] [--force]
# --bundle pastes a foreign-setup string: empty flags are filled from it,
# explicit flags always win.
cli_add_peer() {
    local NAME="" LOCAL_PUB="" REMOTE_PUB="" FRP_PORT="" TOKEN="" LOCAL_GRE="" PEER_GRE="" PORTS="" FORCE=0 BUNDLE=""
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --name) NAME="$2"; shift 2 ;;
            --local-pub) LOCAL_PUB="$2"; shift 2 ;;
            --remote-pub) REMOTE_PUB="$2"; shift 2 ;;
            --frp-port) FRP_PORT="$2"; shift 2 ;;
            --token) TOKEN="$2"; shift 2 ;;
            --local-gre) LOCAL_GRE="$2"; shift 2 ;;
            --peer-gre) PEER_GRE="$2"; shift 2 ;;
            --ports) PORTS="$2"; shift 2 ;;
            --bundle) BUNDLE="$2"; shift 2 ;;
            --chaff) CHAFF_PROFILE="$2"; shift 2 ;;
            --force) FORCE=1; shift ;;
            -h|--help) echo 'Usage: hashem.sh add-peer --local-pub IP --remote-pub IP [--frp-port N] --token T --local-gre IP --peer-gre IP --ports "443, 2083" [--name LABEL] [--bundle hsh1_...] [--chaff low|mid|off] [--force]'; return 0 ;;
            *) echo -e "${RED}[!] Unknown flag: $1${NC}"; return 1 ;;
        esac
    done
    CHAFF_PROFILE="${CHAFF_PROFILE:-$(perf_get_chaff)}"
    case "$CHAFF_PROFILE" in
        low|mid|off) ;;
        *) echo -e "${YELLOW}[!] Unknown chaff profile '${CHAFF_PROFILE}', defaulting to low.${NC}"; CHAFF_PROFILE="low" ;;
    esac
    if [[ -n "$BUNDLE" ]]; then
        bundle_parse "$BUNDLE" || { echo -e "${RED}[!] Bad --bundle (want hsh1_<IRAN_PUB>_<PORT>_<IRAN_GRE>_<FOREIGN_GRE>_<TOKEN>[_<PORTS>]).${NC}"; return 1; }
        # add-peer runs on Iran: bundle Iran pub/GRE are OURS, foreign GRE is THEIRS
        [[ -z "$LOCAL_PUB" ]] && LOCAL_PUB=$B_IRAN_PUB
        [[ -z "$FRP_PORT" ]] && FRP_PORT=$B_FRP_PORT
        [[ -z "$LOCAL_GRE" ]] && LOCAL_GRE=$B_IRAN_GRE
        [[ -z "$PEER_GRE" ]] && PEER_GRE=$B_FOREIGN_GRE
        [[ -z "$TOKEN" ]] && TOKEN=$B_TOKEN
        [[ -z "$PORTS" ]] && PORTS=$B_PORTS
    fi
    FRP_PORT=${FRP_PORT:-$(gen_random_port)}
    validate_setup_common "$LOCAL_PUB" "$REMOTE_PUB" "$FRP_PORT" "$LOCAL_GRE" || return 1
    is_valid_ip "$PEER_GRE" || { echo -e "${RED}[!] Invalid peer GRE IP: '$PEER_GRE'${NC}"; return 1; }
    [[ "$LOCAL_GRE" != "$PEER_GRE" ]] || { echo -e "${RED}[!] Local and peer GRE IPs must differ.${NC}"; return 1; }
    [[ -n "$TOKEN" ]] || { echo -e "${RED}[!] --token is required (generate one per peer).${NC}"; return 1; }
    local CLEANED="" p
    for p in $(echo "$PORTS" | tr ',' ' '); do
        is_valid_port "$p" && CLEANED="$CLEANED $((10#$p))"
    done
    CLEANED=$(echo "$CLEANED" | xargs)
    [[ -n "$CLEANED" ]] || { echo -e "${RED}[!] --ports needs at least one valid port.${NC}"; return 1; }
    peer_init; peer_require_py || return 1
    local ID
    ID=$(peer_next_id)
    [[ "$ID" -ge 1 ]] || { echo -e "${RED}[!] Peer table full (max ${MAX_PEERS} foreign servers). Remove one first.${NC}"; return 1; }
    # port conflict: a remotePort can be served by only one frpc
    local USED entry CONFLICT=""
    USED=$(peer_ports_used)
    for p in $CLEANED; do
        for entry in $USED; do
            if [[ "${entry%%:*}" == "$p" ]]; then CONFLICT="$CONFLICT $p (used by peer '${entry#*:}')"; fi
        done
    done
    if [[ -n "$CONFLICT" ]]; then
        echo -e "${RED}[!] Port conflict — already claimed by another tunnel:${CONFLICT}${NC}"
        echo -e "${YELLOW}    Pick a different port for this peer (e.g. 8443 instead of 443).${NC}"
        return 1
    fi
    # control port must be free on this machine
    if ss -tln 2>/dev/null | grep -q ":${FRP_PORT} "; then
        echo -e "${RED}[!] Control port ${FRP_PORT} is already in use on this server — use another one.${NC}"
        return 1
    fi
    # GRE inner IPs must be unique across peers
    if grep -q "\"local_gre\": *\"${LOCAL_GRE}\"" "$PEERS_FILE" || grep -q "\"peer_gre\": *\"${LOCAL_GRE}\"" "$PEERS_FILE"; then
        echo -e "${RED}[!] GRE IP ${LOCAL_GRE} is already used by another peer.${NC}"; return 1
    fi
    [[ -z "$NAME" ]] && NAME="peer-${ID}"
    install_frp_binaries || return 1
    if [[ "$ID" -eq 1 ]] && ! tunnel_present; then
        # first tunnel keeps legacy names (gre-tunnel, frps) — old setups untouched
        setup_gre_systemd "$LOCAL_PUB" "$REMOTE_PUB" "$LOCAL_GRE"
        peer_write_frps "" "$FRP_PORT" "$TOKEN"
        GRE_IF="$TUNNEL_NAME"; FRPS_SVC="frps"; LEGACY=true
        setup_chaff "" "$PEER_GRE"
    else
        GRE_IF="gre-t${ID}"; FRPS_SVC="frps-${ID}"; LEGACY=false
        setup_gre_iface "$GRE_IF" "$LOCAL_PUB" "$REMOTE_PUB" "$LOCAL_GRE"
        peer_write_frps "-${ID}" "$FRP_PORT" "$TOKEN"
        # point the new unit at the right interface
        sed -i "s/After=network.target/After=network.target ${GRE_IF}.service/" /etc/systemd/system/${FRPS_SVC}.service
        systemctl daemon-reload; systemctl restart "$FRPS_SVC"
        setup_chaff "-${ID}" "$PEER_GRE"
    fi
    # registry record (ports as JSON array)
    local PORTS_JSON
    PORTS_JSON=$(echo "$CLEANED" | python3 -c 'import json,sys; print(json.dumps([int(x) for x in sys.stdin.read().split()]))')
    PEERS_F="$PEERS_FILE" python3 - "$ID" "$NAME" "$LOCAL_PUB" "$REMOTE_PUB" "$FRP_PORT" "$TOKEN" "$LOCAL_GRE" "$PEER_GRE" "$PORTS_JSON" "$GRE_IF" "$FRPS_SVC" "$LEGACY" "${CHAFF_PROFILE:-low}" <<'PYEOF'
import json, os, sys
f = os.environ["PEERS_F"]
iid, name, lip, rip, fport, tok, lgre, pgre, pjson, gif, svc, leg, prof = sys.argv[1:]
d = json.load(open(f))
d.setdefault("peers", []).append({"id": int(iid), "name": name, "local_pub": lip,
  "remote_pub": rip, "frp_port": int(fport), "token": tok, "local_gre": lgre,
  "peer_gre": pgre, "ports": json.loads(pjson), "gre_if": gif, "frps_svc": svc,
  "legacy": leg == "true", "chaff_profile": prof})
json.dump(d, open(f, "w"), indent=2)
PYEOF
    echo -e "${GREEN}[✔️] Peer '${NAME}' (id ${ID}) added: GRE ${LOCAL_PUB} <-> ${REMOTE_PUB} (${LOCAL_GRE} peer ${PEER_GRE} on ${GRE_IF}), ${FRPS_SVC} :${FRP_PORT}${NC}"
    echo -e "${YELLOW}Token for '${NAME}': ${TOKEN} (enter it on the FOREIGN side with ports: ${CLEANED})${NC}"
    echo -e "BUNDLE:$(bundle_make "$LOCAL_PUB" "$FRP_PORT" "$LOCAL_GRE" "$PEER_GRE" "$TOKEN" "$CLEANED")"
    echo -e "${CYAN}Foreign side: frpc server ${LOCAL_GRE}:${FRP_PORT}${NC}"
}

# remove one peer ($1=id). Legacy peer 1 also drops the old single tunnel.
cli_remove_peer() {
    local ID="" FORCE=0
    while [[ $# -gt 0 ]]; do
        case "$1" in --id) ID="$2"; shift 2 ;; --force) FORCE=1; shift ;;
            -h|--help) echo 'Usage: hashem.sh remove-peer --id N [--force]'; return 0 ;;
            *) echo -e "${RED}[!] Unknown flag: $1${NC}"; return 1 ;; esac
    done
    [[ "$ID" =~ ^[0-9]+$ ]] || { echo -e "${RED}[!] --id N is required.${NC}"; return 1; }
    peer_init; peer_require_py || return 1
    local rec
    rec=$(peer_get "$ID")
    [[ -n "$rec" ]] || { echo -e "${RED}[!] No peer with id $ID.${NC}"; return 1; }
    local NAME GIF SVC LEG
    NAME=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin)["name"])')
    GIF=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin)["gre_if"])')
    SVC=$(echo "$rec" | python3 -c 'import json,sys; print(json.load(sys.stdin)["frps_svc"])')
    LEG=$(echo "$rec" | python3 -c 'import json,sys; print("1" if json.load(sys.stdin).get("legacy") else "0")')
    if [[ "$FORCE" -ne 1 ]]; then
        read -p "Remove peer '${NAME}' (id ${ID})? GRE + its frps go away. (y/N): " CONFIRM
        [[ "$CONFIRM" =~ ^[Yy]$ ]] || { echo -e "${YELLOW}[*] Aborted.${NC}"; return 0; }
    fi
    if [[ "$LEG" == "1" ]]; then
        remove_tunnel_force
    else
        systemctl stop "$SVC" "${GIF}.service" "gre-chaff-${ID}.service" >/dev/null 2>&1
        systemctl disable "$SVC" "${GIF}.service" "gre-chaff-${ID}.service" >/dev/null 2>&1
        rm -f "/etc/systemd/system/${SVC}.service" "/etc/systemd/system/${GIF}.service" "/etc/frp/frps-${ID}.toml" "/etc/systemd/system/gre-chaff-${ID}.service"
        systemctl daemon-reload; systemctl reset-failed >/dev/null 2>&1 || true
        ip tunnel del "$GIF" >/dev/null 2>&1 || true
    fi
    PEERS_F="$PEERS_FILE" PEER_ID="$ID" python3 -c \
'import json,os; f=os.environ["PEERS_F"]; d=json.load(open(f)); d["peers"]=[p for p in d.get("peers",[]) if p["id"]!=int(os.environ["PEER_ID"])]; json.dump(d,open(f,"w"),indent=2)' \
        || echo -e "${YELLOW}[!] peers registry already gone — nothing left to clean.${NC}"
    echo -e "${GREEN}[✔️] Peer '${NAME}' (id ${ID}) removed.${NC}"
}

# readable peer table for the menu
peer_list_pretty() {
    peer_init; peer_require_py || return 1
    PEERS_F="$PEERS_FILE" python3 <<'PYEOF'
import json, os, subprocess
try:
    peers = json.load(open(os.environ["PEERS_F"])).get("peers", [])
except Exception as e:
    print(f"[!] cannot read peers registry: {e}"); raise SystemExit(1)
if not peers:
    print("[*] No peer tunnels yet. Use 'Add peer tunnel' to connect a foreign server.")
    raise SystemExit(0)
tun = subprocess.run(["ip", "tunnel", "show"], capture_output=True, text=True).stdout
for p in sorted(peers, key=lambda x: x["id"]):
    gre = "up" if p.get("gre_if", "") in tun else "down"
    try:
        frp = subprocess.run(["systemctl", "is-active", p.get("frps_svc", "")],
                             capture_output=True, text=True).stdout.strip()
    except Exception:
        frp = "?"
    print(f"#{p['id']} {p['name']}: {p['remote_pub']} (GRE {p['local_gre']} peer {p['peer_gre']}, {p['gre_if']} {gre}) "
          f"| {p['frps_svc']} :{p['frp_port']} {frp} | ports: {','.join(map(str, p.get('ports', [])))}")
PYEOF
}

setup_iran_server() {
    echo -e "\n${YELLOW}====================================================${NC}"
    echo -e "${YELLOW}       STEP 1: CONFIGURING IRAN SERVER (GRE + FRPS)  ${NC}"
    echo -e "${YELLOW}====================================================${NC}"

    # Prefer the local interface IP (what GRE must bind to) over the egress IP
    # an external service sees (often different behind NAT, e.g. ipify).
    MY_PUBLIC_IP=$(ip route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}')
    [[ -z "$MY_PUBLIC_IP" ]] && MY_PUBLIC_IP=$(curl -sSL --max-time 5 https://api.ipify.org 2>/dev/null)
    prompt_ip IP_IRAN "Enter IRAN Server Public IP" "$MY_PUBLIC_IP"
    prompt_ip IP_FOREIGN "Enter FOREIGN Server Public IP" ""

    prompt_port BIND_PORT "Enter FRP Bind Port" "$(gen_random_port)"

    AUTO_TOKEN=$(gen_token32)
    prompt_token TOKEN "Enter Secret Auth Token" "$AUTO_TOKEN"

    # single source of truth: GRE + frps + panel all happen inside
    setup_iran_server_noninteractive "$IP_IRAN" "$IP_FOREIGN" "$BIND_PORT" "$TOKEN" "$IRAN_GRE_IP" "$FOREIGN_GRE_IP"

    echo -e "\n${GREEN}=================================================================${NC}"
    echo -e "${GREEN}[✔️] IRAN SERVER CONFIGURATION COMPLETE!${NC}"
    echo -e "GRE Public Link:      ${CYAN}${IP_IRAN} <--> ${IP_FOREIGN}${NC}"
    echo -e "IRAN GRE Internal IP: ${CYAN}${IRAN_GRE_IP}${NC}"
    echo -e "FRP Bind Port:        ${CYAN}${BIND_PORT}${NC}"
    echo -e "Secret Token:         ${CYAN}${TOKEN}${NC}"
    echo -e "Setup Bundle:         ${CYAN}$(bundle_make "$IP_IRAN" "$BIND_PORT" "$IRAN_GRE_IP" "$FOREIGN_GRE_IP" "$TOKEN")${NC}"
    echo -e "\n${YELLOW}>>> Now run this script on FOREIGN server and provide:${NC}"
    echo -e "Paste the ${CYAN}Setup Bundle${NC} above (has IP + port + GRE + token) — or manually:"
    echo -e "1. IRAN Public IP: ${CYAN}${IP_IRAN}${NC}"
    echo -e "2. Port:           ${CYAN}${BIND_PORT}${NC}"
    echo -e "3. Token:          ${CYAN}${TOKEN}${NC}"
    echo -e "${GREEN}=================================================================${NC}\n"

    # panel is already running here (menu path) — install it fresh
    install_panel || echo -e "${YELLOW}[!] Panel auto-install failed — retry from menu option 15 (Update All).${NC}"
}

# interactive wrapper for cli_add_peer: prompts for one more foreign server.
menu_add_peer() {
    echo -e "\n${YELLOW}=== Add Peer Tunnel (connect ANOTHER foreign server to this Iran) ===${NC}"
    peer_init
    USED=$(peer_ports_used 2>/dev/null)
    [[ -n "$USED" ]] && echo -e "${CYAN}Already claimed reverse ports: ${USED}${NC}"
    local MYIP
    MYIP=$(ip route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}')
    local NAME IP_FOREIGN PORT CPORT TOKEN LGRE PGRE PPORTS
    read -p "Peer name (e.g. germany-1) [Enter for auto]: " NAME
    prompt_ip LOCAL_IRAN "Enter IRAN Server Public IP" "$MYIP"
    prompt_ip IP_FOREIGN "Enter FOREIGN Server Public IP" ""
    # suggest next free control port + GRE pair
    local NEXT_ID SU_FP SU_LG SU_PG
    NEXT_ID=$(peer_next_id 2>/dev/null || echo 2)
    SU_FP=$(gen_random_port)
    SU_LG="10.1${NEXT_ID}.0.2"; SU_PG="10.1${NEXT_ID}.0.1"
    prompt_port CPORT "Enter FRP Control Port (unique per peer)" "$SU_FP"
    AUTO_TOKEN=$(gen_token32)
    prompt_token TOKEN "Peer token (each peer gets its own)" "$AUTO_TOKEN"
    prompt_ip LGRE "Local GRE IP (unique per peer)" "$SU_LG"
    prompt_ip PGRE "Peer GRE IP" "$SU_PG"
    prompt_ports PPORTS "Ports to Reverse-Tunnel"
    cli_add_peer --name "$NAME" --local-pub "$LOCAL_IRAN" --remote-pub "$IP_FOREIGN" \
        --frp-port "$CPORT" --token "$TOKEN" --local-gre "$LGRE" --peer-gre "$PGRE" --ports "$PPORTS"
    echo -e "\n${GREEN}=== On the FOREIGN server, run this script option 2 with: ===${NC}"
    echo -e "IRAN Public IP: ${CYAN}${LOCAL_IRAN}${NC} | Port: ${CYAN}${CPORT}${NC} | Token: ${CYAN}${TOKEN}${NC}"
    echo -e "GRE: local ${CYAN}${PGRE}${NC} peer ${CYAN}${LGRE}${NC} | Ports: ${CYAN}${PPORTS}${NC}"
}

menu_remove_peer() {
    echo -e "\n${YELLOW}=== Remove Peer Tunnel ===${NC}"
    peer_list_pretty || return 1
    local ID
    read -p "Peer id to remove: " ID
    cli_remove_peer --id "$ID"
}

setup_foreign_server() {
    echo -e "\n${YELLOW}====================================================${NC}"
    echo -e "${YELLOW}   STEP 2: CONFIGURING FOREIGN SERVER (GRE + FRPC)  ${NC}"
    echo -e "${YELLOW}====================================================${NC}"
    MY_PUBLIC_IP=$(ip route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}')
    [[ -z "$MY_PUBLIC_IP" ]] && MY_PUBLIC_IP=$(curl -sSL --max-time 5 https://api.ipify.org 2>/dev/null)
    prompt_ip IP_FOREIGN "Enter FOREIGN Server Public IP" "$MY_PUBLIC_IP"
    prompt_ip IP_IRAN "Enter IRAN Server Public IP" ""
    # bundle shortcut: paste hsh1_... -> everything auto-fills, rest is skipped
    local BUNDLE_IN=""
    read -p "Setup bundle from Iran (hsh1_...) [Enter to fill fields manually]: " BUNDLE_IN
    local SERVER_PORT TOKEN INPUT_PORTS BUNDLE_USED=0 LOCAL_GRE_SET="$FOREIGN_GRE_IP" PEER_GRE_SET="$IRAN_GRE_IP"
    if [[ -n "$BUNDLE_IN" ]]; then
        if bundle_parse "$BUNDLE_IN"; then
            IP_IRAN=$B_IRAN_PUB; IP_FOREIGN=${MY_PUBLIC_IP:-$IP_FOREIGN}
            SERVER_PORT=$B_FRP_PORT; TOKEN=$B_TOKEN
            LOCAL_GRE_SET=$B_FOREIGN_GRE; PEER_GRE_SET=$B_IRAN_GRE
            INPUT_PORTS=$(echo "$B_PORTS" | tr ' ' ',')
            BUNDLE_USED=1
            echo -e "${GREEN}[✔️] Bundle applied: Iran ${IP_IRAN}:${SERVER_PORT}, token set, ports: ${INPUT_PORTS:-— (enter below)}${NC}"
        else
            echo -e "${RED}[!] Bad bundle — falling back to manual fields.${NC}"
        fi
    fi
    if [[ "$BUNDLE_USED" -ne 1 ]]; then
        prompt_port SERVER_PORT "Enter FRP Bind Port" "$(gen_random_port)"
        prompt_required TOKEN "Enter Secret Auth Token"
        prompt_ports INPUT_PORTS "Enter Ports to Reverse-Tunnel"
    elif [[ -z "$INPUT_PORTS" ]]; then
        prompt_ports INPUT_PORTS "Enter Ports to Reverse-Tunnel"
    fi

    # single source of truth: GRE + ping + frpc + panel all happen inside
    # (frpc reaches Iran's GRE internal IP through the GRE tunnel)
    PORTS_CLEANED=$(echo "$INPUT_PORTS" | tr ',' ' ')
    _setup_foreign_full "$IP_FOREIGN" "$IP_IRAN" "$SERVER_PORT" "$TOKEN" "$LOCAL_GRE_SET" "$PEER_GRE_SET" "$PORTS_CLEANED"

    echo -e "\n${GREEN}=================================================================${NC}"
    echo -e "${GREEN}[✔️] FOREIGN SERVER CONFIGURATION COMPLETE!${NC}"
    echo -e "GRE Public Link:      ${CYAN}${IP_FOREIGN} <--> ${IP_IRAN}${NC}"
    echo -e "FOREIGN GRE IP:       ${CYAN}${LOCAL_GRE_SET}${NC}"
    echo -e "FRP Connecting to:    ${CYAN}${PEER_GRE_SET}:${SERVER_PORT}${NC} (Inside GRE Tunnel)"
    echo -e "Reverse Ports:        ${CYAN}${PORTS_CLEANED}${NC} (TCP & UDP)"
    echo -e "FRP TLS Encryption:   ${GREEN}Enabled${NC}"
    echo -e "${GREEN}=================================================================${NC}\n"

    # panel is already running here (menu path) — install it fresh
    install_panel || echo -e "${YELLOW}[!] Panel auto-install failed — retry from menu option 15 (Update All).${NC}"
}

check_status() {
    echo -e "\n${YELLOW}=== Checking GRE & FRP Status ===${NC}"

    # 1. GRE Status
    echo -e "\n${CYAN}[1] GRE Tunnel Interface:${NC}"
    if ip link show "$TUNNEL_NAME" >/dev/null 2>&1; then
        ip addr show dev "$TUNNEL_NAME"
        echo -e "${GREEN}[✔️] Interface ${TUNNEL_NAME} exists and is UP.${NC}"
    else
        echo -e "${RED}[!] Interface ${TUNNEL_NAME} NOT found.${NC}"
    fi

    # 2. Ping Test
    echo -e "\n${CYAN}[2] GRE Ping Test:${NC}"
    if ip addr show dev "$TUNNEL_NAME" 2>/dev/null | grep -q "$IRAN_GRE_IP"; then
        TARGET_PING="$FOREIGN_GRE_IP"
        echo "Testing ping to Foreign GRE IP ($TARGET_PING)..."
    else
        TARGET_PING="$IRAN_GRE_IP"
        echo "Testing ping to Iran GRE IP ($TARGET_PING)..."
    fi
    ping -c 3 -W 2 "$TARGET_PING" && echo -e "${GREEN}[✔️] Ping OK.${NC}" || echo -e "${YELLOW}[!] Remote peer did not answer ping.${NC}"

    # 3. FRP Service Status
    echo -e "\n${CYAN}[3] FRP Service Status:${NC}"
    if systemctl is-active --quiet frps; then
        echo -e "${GREEN}[✔️] frps (Server on IRAN) is ACTIVE and RUNNING.${NC}"
        systemctl status frps --no-pager -l
    elif systemctl is-active --quiet frpc; then
        echo -e "${GREEN}[✔️] frpc (Client on FOREIGN) is ACTIVE and RUNNING.${NC}"
        systemctl status frpc --no-pager -l
    else
        echo -e "${RED}[!] Neither frps nor frpc is active.${NC}"
    fi
}

show_logs() {
    echo -e "\n${YELLOW}=== Live Service Logs (Ctrl+C to exit) ===${NC}"
    if systemctl list-unit-files | grep -q "frps.service"; then
        journalctl -u frps -n 50 -f
    elif systemctl list-unit-files | grep -q "frpc.service"; then
        journalctl -u frpc -n 50 -f
    else
        echo -e "${RED}[!] No FRP service found.${NC}"
    fi
}

restart_all() {
    echo -e "\n${CYAN}[*] Restarting GRE and FRP services (all tunnels)...${NC}"
    local u
    for u in /etc/systemd/system/gre-t*.service /etc/systemd/system/gre-tunnel.service /etc/systemd/system/frps*.service /etc/systemd/system/frpc.service /etc/systemd/system/gre-chaff*.service; do
        [[ -f "$u" ]] || continue
        systemctl restart "$(basename "$u")" >/dev/null 2>&1 && echo -e "${GREEN}[✔️] $(basename "$u") restarted.${NC}"
    done
    echo -e "${GREEN}[✔️] All services restarted.${NC}"
}

uninstall_all() {
    echo -e "\n${RED}=== Uninstalling EVERYTHING (tunnel + panel + hashem command) ===${NC}"
    read -p "Are you sure? This removes GRE & FRP, the web panel AND the 'hashem' command. (y/N): " CONFIRM
    if [[ "$CONFIRM" =~ ^[Yy]$ ]]; then
        uninstall_all_force
    else
        echo -e "${YELLOW}[*] Aborted.${NC}"
    fi
}

# Non-interactive core: full wipe. Called by uninstall_all() after confirm
# and by `hashem uninstall --force`. Must also delete the menu entrypoints
# (/usr/local/bin/hashem + /usr/local/bin/hashem.sh + legacy gre.sh) so
# `hashem` stops working.
uninstall_all_force() {
        # Stop & disable services (legacy + all peers + panel + chaff + watchdog)
        systemctl stop frps frpc "${TUNNEL_NAME}.service" gre-panel gre-chaff hashem-watchdog.timer hashem-watchdog.service >/dev/null 2>&1
        systemctl stop 'frps@*' 'frpc@*' 'gre-t*.service' 'gre-chaff*.service' >/dev/null 2>&1 || true
        systemctl disable frps frpc "${TUNNEL_NAME}.service" gre-panel gre-chaff hashem-watchdog.timer 'gre-chaff*.service' >/dev/null 2>&1 || true

        # Remove systemd files
        cli_dpi_shield off >/dev/null 2>&1 || true
        rm -f /etc/systemd/system/frps*.service /etc/systemd/system/frpc.service /etc/systemd/system/${TUNNEL_NAME}.service /etc/systemd/system/gre-t*.service /etc/systemd/system/gre-panel.service /etc/systemd/system/gre-chaff*.service /etc/systemd/system/hashem-watchdog.* /etc/systemd/system/hashem-dpi.service
        rm -f /var/lock/hashem-watchdog.lock
        systemctl daemon-reload
        systemctl reset-failed >/dev/null 2>&1 || true

        # Remove GRE interfaces (legacy + all peers)
        local gif
        for gif in "$TUNNEL_NAME" $(ip tunnel show 2>/dev/null | grep -o 'gre-t[0-9]*'); do
            ip tunnel del "$gif" >/dev/null 2>&1 || true
        done

        # Remove tunnel binaries & configs (including peers registry)
        rm -f "${INSTALL_DIR}/frps" "${INSTALL_DIR}/frpc"
        rm -rf "$CONFIG_DIR"
        rm -f "$PEERS_FILE"

        # Remove chaff script
        rm -f /usr/local/bin/hashem-chaff.sh /usr/local/bin/gre-chaff.sh

        # Remove web panel (service + binary + config + helper CLIs)
        rm -f /usr/local/bin/gre-panel /usr/local/bin/grepanel
        rm -rf /etc/gre-panel /usr/local/gre-panel

        # Remove the menu entrypoints LAST so `hashem` stops opening a menu.
        # (Deleting a running script's own file is safe on Linux — the open fd stays valid.)
        rm -f /usr/local/bin/hashem /usr/local/bin/hashem.sh /usr/local/bin/gre.sh

        echo -e "${GREEN}[✔️] Everything uninstalled: tunnel + panel + 'hashem' command removed.${NC}"
}

remove_tunnel() {
    echo -e "\n${RED}=== Removing GRE + FRP Tunnel (panel stays) ===${NC}"
    read -p "Remove the tunnel from THIS server? Panel stays installed. (y/N): " CONFIRM
    if [[ "$CONFIRM" =~ ^[Yy]$ ]]; then
        remove_tunnel_force
    else
        echo -e "${YELLOW}[*] Aborted.${NC}"
    fi
}

# Non-interactive core: stop/disable units, drop interface, remove FRP files.
# Panel files/services are never touched here.
remove_tunnel_force() {
        # Stop & disable services
        systemctl stop frps frpc "${TUNNEL_NAME}.service" gre-chaff >/dev/null 2>&1
        systemctl stop 'gre-chaff*.service' >/dev/null 2>&1 || true
        systemctl disable frps frpc "${TUNNEL_NAME}.service" gre-chaff 'gre-chaff*.service' >/dev/null 2>&1 || true

        # Remove systemd files (legacy + all peer tunnels + chaff + dpi shield)
        cli_dpi_shield off >/dev/null 2>&1 || true
        rm -f /etc/systemd/system/frps*.service /etc/systemd/system/frpc.service /etc/systemd/system/${TUNNEL_NAME}.service /etc/systemd/system/gre-t*.service /etc/systemd/system/gre-chaff*.service /etc/systemd/system/hashem-dpi.service
        systemctl daemon-reload
        systemctl reset-failed >/dev/null 2>&1 || true

        # Remove GRE interfaces (legacy + all peers)
        local gif
        for gif in "$TUNNEL_NAME" $(ip tunnel show 2>/dev/null | grep -o 'gre-t[0-9]*'); do
            ip tunnel del "$gif" >/dev/null 2>&1 || true
        done

        # Remove binaries & configs (panel untouched, peers registry cleared)
        rm -f "${INSTALL_DIR}/frps" "${INSTALL_DIR}/frpc"
        rm -rf "$CONFIG_DIR"
        rm -f "$PEERS_FILE"

        echo -e "${GREEN}[✔️] Tunnel removed — GRE interface, FRP services, binaries and configs gone. Panel still running.${NC}"
}

PANEL_DIR="/usr/local/gre-panel"
PANEL_BIN="/usr/local/bin/gre-panel"

# ---- Network optimization for tunnel throughput ----
# Same on both roles (auto-detects nothing: these are role-independent).
# Backup lives in /etc/gre-panel/tune.bak (key=value snapshot), restored by
# tune_restore(). Idempotent — safe to run twice.
TUNE_BACKUP="/etc/gre-panel/tune.bak"

tune_backup_once() {
    if [[ -f "$TUNE_BACKUP" ]]; then return 0; fi
    mkdir -p "$(dirname "$TUNE_BACKUP")"
    : > "$TUNE_BACKUP"
    local k v
    for k in net.ipv4.ip_forward net.core.rmem_max net.core.wmem_max \
             net.core.netdev_max_backlog net.ipv4.tcp_congestion_control; do
        v=$(sysctl -n "$k" 2>/dev/null) || v=""
        echo "$k=$v" >> "$TUNE_BACKUP"
    done
    if lsmod 2>/dev/null | grep -q "^tcp_bbr"; then echo "tcp_bbr=loaded" >> "$TUNE_BACKUP";
    else echo "tcp_bbr=absent" >> "$TUNE_BACKUP"; fi
    echo "gre_mtu=$(ip link show "$TUNNEL_NAME" 2>/dev/null | grep -o 'mtu [0-9]*' | awk '{print $2}')" >> "$TUNE_BACKUP"
    if iptables -t mangle -C POSTROUTING -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu >/dev/null 2>&1; then
        echo "mss_clamp=present" >> "$TUNE_BACKUP"
    else
        echo "mss_clamp=absent" >> "$TUNE_BACKUP"
    fi
    echo -e "${CYAN}[*] Current settings backed up to ${TUNE_BACKUP}.${NC}"
}

tune_apply() {
    tune_backup_once
    echo -e "${CYAN}[*] Optimizing network stack for tunnel throughput...${NC}"

    # 1. BBR congestion control (best for high-latency links like IR↔TR)
    if modprobe tcp_bbr >/dev/null 2>&1 || lsmod 2>/dev/null | grep -q "^tcp_bbr"; then
        sysctl -w net.ipv4.tcp_congestion_control=bbr >/dev/null 2>&1 && echo -e "${GREEN}[✔️] TCP congestion control → bbr${NC}" || echo -e "${YELLOW}[!] bbr unavailable — keeping current CC.${NC}"
    else
        echo -e "${YELLOW}[!] tcp_bbr module not available — keeping current CC.${NC}"
    fi

    # 2. Bigger socket buffers (16MB) so fast links don't stall
    sysctl -w net.core.rmem_max=16777216 >/dev/null 2>&1
    sysctl -w net.core.wmem_max=16777216 >/dev/null 2>&1
    echo -e "${GREEN}[✔️] Socket buffers → 16MB (rmem_max/wmem_max)${NC}"

    # 3. Deeper NIC queue (packet bursts under load)
    sysctl -w net.core.netdev_max_backlog=5000 >/dev/null 2>&1
    echo -e "${GREEN}[✔️] netdev backlog → 5000${NC}"

    # 4. IP forwarding (tunnel needs it)
    sysctl -w net.ipv4.ip_forward=1 >/dev/null 2>&1
    echo -e "${GREEN}[✔️] IPv4 forwarding → on${NC}"

    # 5. GRE MTU 1448 (1500 outer − 24 GRE − 28 IP/ICMP headroom:
    # full-size packets pass unfragmented, verified by MTU probe)
    if ip link show "$TUNNEL_NAME" >/dev/null 2>&1; then
        ip link set dev "$TUNNEL_NAME" mtu 1448 >/dev/null 2>&1 && echo -e "${GREEN}[✔️] ${TUNNEL_NAME} MTU → 1448${NC}" || echo -e "${YELLOW}[!] Could not set GRE MTU.${NC}"
    else
        echo -e "${YELLOW}[*] No ${TUNNEL_NAME} interface yet — MTU will apply on next setup.${NC}"
    fi

    # 6. MSS clamp (idempotent) so TCP never fragments through the tunnel
    iptables -t mangle -C POSTROUTING -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu >/dev/null 2>&1 || \
        iptables -t mangle -A POSTROUTING -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu
    echo -e "${GREEN}[✔️] TCP MSS clamp → on${NC}"

    # 7. Persist across reboots
    mkdir -p /etc/sysctl.d
    cat > /etc/sysctl.d/99-gre-tune.conf <<'EOF'
# Hashem tunnel optimization (applied by Optimize button / tune command)
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.core.netdev_max_backlog = 5000
net.ipv4.ip_forward = 1
EOF
    if sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null | grep -q bbr; then
        echo "net.ipv4.tcp_congestion_control = bbr" >> /etc/sysctl.d/99-gre-tune.conf
    fi
    echo -e "${GREEN}[✔️] Settings persisted in /etc/sysctl.d/99-gre-tune.conf${NC}"
    echo -e "${GREEN}[✔️] Optimization done — run Restore if anything feels worse.${NC}"
}

tune_restore() {
    if [[ ! -f "$TUNE_BACKUP" ]]; then
        echo -e "${YELLOW}[!] No backup found at ${TUNE_BACKUP} — nothing to restore.${NC}"
        return 1
    fi
    echo -e "${CYAN}[*] Restoring pre-optimization settings...${NC}"
    local k v
    while IFS='=' read -r k v; do
        case "$k" in
            net.*) [[ -n "$v" ]] && sysctl -w "$k=$v" >/dev/null 2>&1 && echo -e "${GREEN}[✔️] $k → $v${NC}" ;;
            gre_mtu)
                if [[ -n "$v" ]] && ip link show "$TUNNEL_NAME" >/dev/null 2>&1; then
                    ip link set dev "$TUNNEL_NAME" mtu "$v" >/dev/null 2>&1 && echo -e "${GREEN}[✔️] ${TUNNEL_NAME} MTU → $v${NC}"
                fi ;;
            mss_clamp)
                if [[ "$v" == "absent" ]]; then
                    iptables -t mangle -D POSTROUTING -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu >/dev/null 2>&1 || true
                    echo -e "${GREEN}[✔️] MSS clamp removed${NC}"
                fi ;;
        esac
    done < "$TUNE_BACKUP"
    rm -f /etc/sysctl.d/99-gre-tune.conf
    echo -e "${GREEN}[✔️] Restored — backup kept at ${TUNE_BACKUP} (deleted on next optimize run).${NC}"
    rm -f "$TUNE_BACKUP"
}

tune_status() {
    echo -e "${CYAN}=== Tunnel Optimization Status ===${NC}"
    echo "CC:        $(sysctl -n net.ipv4.tcp_congestion_control 2>/dev/null || echo ?)"
    echo "rmem_max:  $(sysctl -n net.core.rmem_max 2>/dev/null || echo ?)"
    echo "wmem_max:  $(sysctl -n net.core.wmem_max 2>/dev/null || echo ?)"
    echo "backlog:   $(sysctl -n net.core.netdev_max_backlog 2>/dev/null || echo ?)"
    echo "forward:   $(sysctl -n net.ipv4.ip_forward 2>/dev/null || echo ?)"
    echo "GRE MTU:   $(ip link show "$TUNNEL_NAME" 2>/dev/null | grep -o 'mtu [0-9]*' | awk '{print $2}' || echo 'no interface')"
    if iptables -t mangle -C POSTROUTING -p tcp --tcp-flags SYN,RST SYN -j TCPMSS --clamp-mss-to-pmtu >/dev/null 2>&1; then
        echo "MSS clamp: on"
    else
        echo "MSS clamp: off"
    fi
    if [[ -f "$TUNE_BACKUP" ]]; then echo "Backup:    $TUNE_BACKUP (restore available)"; else echo "Backup:    none"; fi
    [[ -f /etc/sysctl.d/99-gre-tune.conf ]] && echo "Persisted: yes (/etc/sysctl.d/99-gre-tune.conf)" || echo "Persisted: no"
}

# free_ram: drop page caches + compact memory + journald cap + ensure 1G swap.
# Safe on any Ubuntu host: no service is touched, kernel reclaims only
# discardable cache; swap is created once and reused afterwards.
free_ram() {
    echo -e "${CYAN}[*] Freeing RAM (safe: caches only, no service touched)...${NC}"
    local before
    before=$(free -m | awk '/^Mem:/{print $7}')
    # 1. journald cap (the #1 silent RAM eater on Ubuntu: 100M+ in RAM)
    if [[ -f /etc/systemd/journald.conf ]]; then
        sed -i 's/^#*SystemMaxUse=.*/SystemMaxUse=32M/' /etc/systemd/journald.conf
        sed -i 's/^#*RuntimeMaxUse=.*/RuntimeMaxUse=16M/' /etc/systemd/journald.conf
        grep -q '^SystemMaxUse=32M' /etc/systemd/journald.conf || echo 'SystemMaxUse=32M' >> /etc/systemd/journald.conf
        grep -q '^RuntimeMaxUse=16M' /etc/systemd/journald.conf || echo 'RuntimeMaxUse=16M' >> /etc/systemd/journald.conf
        journalctl --vacuum-size=16M >/dev/null 2>&1
        systemctl restart systemd-journald >/dev/null 2>&1
        echo -e "${GREEN}[✔️] journald capped at 16M (was the main RAM eater)${NC}"
    fi
    # 2. drop page caches + compact
    sync
    echo 3 > /proc/sys/vm/drop_caches 2>/dev/null
    echo 1 > /proc/sys/vm/compact_memory 2>/dev/null
    echo -e "${GREEN}[✔️] page cache dropped + memory compacted${NC}"
    # 3. ensure 1G swap (safety net for 1GB VPS)
    if ! swapon --show 2>/dev/null | grep -q '/swapfile'; then
        echo -e "${CYAN}[*] Creating 1G swapfile...${NC}"
        if fallocate -l 1G /swapfile 2>/dev/null || dd if=/dev/zero of=/swapfile bs=1M count=1024 2>/dev/null; then
            chmod 600 /swapfile
            mkswap /swapfile >/dev/null 2>&1
            swapon /swapfile >/dev/null 2>&1
            grep -q '/swapfile' /etc/fstab 2>/dev/null || echo '/swapfile none swap sw 0 0' >> /etc/fstab
            echo -e "${GREEN}[✔️] 1G swap created${NC}"
        else
            echo -e "${YELLOW}[!] Could not create swapfile (disk full?)${NC}"
        fi
    else
        echo -e "${GREEN}[✔️] swap already active${NC}"
    fi
    sysctl -w vm.swappiness=15 >/dev/null 2>&1
    echo 'vm.swappiness=15' > /etc/sysctl.d/99-swappiness.conf 2>/dev/null
    local after
    after=$(free -m | awk '/^Mem:/{print $7}')
    echo -e "${GREEN}[✔️] Available RAM: ${before}M → ${after}M${NC}"
    free -m | head -2
}

install_panel() {
    echo -e "${CYAN}[*] Installing Hashem web panel...${NC}"

    ARCH=$(uname -m)
    case "$ARCH" in
        x86_64)  PANEL_ASSET="gre-panel-linux-amd64" ;;
        aarch64|arm64) PANEL_ASSET="gre-panel-linux-arm64" ;;
        *) echo -e "${RED}[!] Unsupported arch for panel: $ARCH${NC}"; return 1 ;;
    esac

    TMP_PANEL="$(mktemp -d)"
    DL_OK=0
    # try latest release first (prebuilt, no Go needed)
    LATEST_JSON=$(curl -fsSL --max-time 15 "https://api.github.com/repos/pdnczone/hashem-panel/releases/latest" 2>/dev/null) || true
    if [[ -n "$LATEST_JSON" ]]; then
        DL_URL=$(echo "$LATEST_JSON" | grep -o "\"browser_download_url\": *\"[^\"]*${PANEL_ASSET}\"" | head -1 | cut -d'"' -f4)
        if [[ -n "$DL_URL" ]] && curl -fsSL --max-time 90 -L "$DL_URL" -o "$TMP_PANEL/gre-panel" && [[ -s "$TMP_PANEL/gre-panel" ]]; then
            if head -c 4 "$TMP_PANEL/gre-panel" | grep -q "ELF"; then
                DL_OK=1
                echo -e "${GREEN}[✔️] Downloaded prebuilt panel ($(du -h "$TMP_PANEL/gre-panel" | cut -f1)).${NC}"
            else
                echo -e "${YELLOW}[!] Downloaded file is not a binary — falling back to source build.${NC}"
            fi
        fi
        GREPANEL_URL=$(echo "$LATEST_JSON" | grep -o "\"browser_download_url\": *\"[^\"]*grepanel\"" | head -1 | cut -d'"' -f4)
        if [[ -n "$GREPANEL_URL" ]]; then
            curl -fsSL --max-time 30 "$GREPANEL_URL" -o /usr/local/bin/grepanel 2>/dev/null && chmod +x /usr/local/bin/grepanel || true
        fi
        HASHEMSH_URL=$(echo "$LATEST_JSON" | grep -o "\"browser_download_url\": *\"[^\"]*hashem\\.sh\"" | head -1 | cut -d'"' -f4)
        if [[ -n "$HASHEMSH_URL" ]]; then
            curl -fsSL --max-time 30 "$HASHEMSH_URL" -o "$HASHEM_SCRIPT" 2>/dev/null && chmod +x "$HASHEM_SCRIPT" || true
            cp "$HASHEM_SCRIPT" "$HASHEM_BIN" 2>/dev/null && chmod +x "$HASHEM_BIN" || true
            ln -sf "$HASHEM_SCRIPT" /usr/local/bin/gre.sh 2>/dev/null || true
        else
            # transitional: releases before the hashem.sh rename ship gre.sh
            GRESH_URL=$(echo "$LATEST_JSON" | grep -o "\"browser_download_url\": *\"[^\"]*gre\\.sh\"" | head -1 | cut -d'"' -f4)
            if [[ -n "$GRESH_URL" ]]; then
                curl -fsSL --max-time 30 "$GRESH_URL" -o "$HASHEM_SCRIPT" 2>/dev/null && chmod +x "$HASHEM_SCRIPT" || true
                cp "$HASHEM_SCRIPT" "$HASHEM_BIN" 2>/dev/null && chmod +x "$HASHEM_BIN" || true
                ln -sf "$HASHEM_SCRIPT" /usr/local/bin/gre.sh 2>/dev/null || true
            fi
        fi
        HASHEM_URL=$(echo "$LATEST_JSON" | grep -o "\"browser_download_url\": *\"[^\"]*/hashem\"" | head -1 | cut -d'"' -f4)
        if [[ -n "$HASHEM_URL" ]]; then
            curl -fsSL --max-time 30 "$HASHEM_URL" -o /usr/local/bin/hashem 2>/dev/null && chmod +x /usr/local/bin/hashem || true
        fi
    fi

    if [[ "$DL_OK" -ne 1 ]]; then
        # fallback: build from source (needs Go)
        echo -e "${YELLOW}[*] No prebuilt panel found — building from source...${NC}"
        if ! command -v go >/dev/null 2>&1; then
            echo -e "${CYAN}[*] Installing Go to build the panel...${NC}"
            apt-get update -qq
            apt-get install -y -qq golang-go
        fi
        if ! curl -fsSL "https://github.com/pdnczone/hashem-panel/archive/refs/heads/main.tar.gz" -o "$TMP_PANEL/panel.tgz"; then
            echo -e "${RED}[!] Failed to download panel sources.${NC}"
            rm -rf "$TMP_PANEL"
            return 1
        fi
        tar -xzf "$TMP_PANEL/panel.tgz" -C "$TMP_PANEL"
        SRC="$(dirname "$(find "$TMP_PANEL" -name main.go -path '*panel*' | head -1)")"
        if [[ -z "$SRC" || ! -f "$SRC/main.go" ]]; then
            echo -e "${RED}[!] Panel sources not found in archive.${NC}"
            rm -rf "$TMP_PANEL"
            return 1
        fi
        (cd "$SRC" && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$TMP_PANEL/gre-panel" .)
        if [[ -f "$SRC/grepanel" ]]; then
            cp "$SRC/grepanel" /usr/local/bin/grepanel
            chmod +x /usr/local/bin/grepanel
        fi
    fi

    cp "$TMP_PANEL/gre-panel" "$PANEL_BIN"
    chmod +x "$PANEL_BIN"
    rm -rf "$TMP_PANEL"
    # /usr/local/bin/hashem IS this script now (no shortcut file anymore):
    # install a copy plus a legacy gre.sh symlink so old muscle memory works.
    cp "$0" "$HASHEM_BIN" 2>/dev/null || cp ./hashem.sh "$HASHEM_BIN" 2>/dev/null || cp "$SRC/hashem.sh" "$HASHEM_BIN" 2>/dev/null || true
    chmod +x "$HASHEM_BIN" 2>/dev/null || true
    ln -sf "$HASHEM_SCRIPT" /usr/local/bin/gre.sh 2>/dev/null || true

    cat > /etc/systemd/system/gre-panel.service <<EOF
[Unit]
Description=Hashem Web Panel
After=network.target

[Service]
Type=simple
User=root
Restart=always
RestartSec=5s
ExecStart=${PANEL_BIN}

[Install]
WantedBy=multi-user.target
EOF

    systemctl daemon-reload
    systemctl enable gre-panel >/dev/null 2>&1
    systemctl restart gre-panel
    sleep 2

    if systemctl is-active --quiet gre-panel; then
        # fresh password is in the log; save it so option 8 can show it
        NEWPASS=$(journalctl -u gre-panel -n 5 --no-pager 2>/dev/null | grep -o 'panel password: [0-9]*' | tail -1 | awk '{print $3}')
        [[ -n "$NEWPASS" ]] && save_panel_pass "$NEWPASS"
        echo -e "${GREEN}[✔️] Panel installed and running.${NC}"
        # full credentials right here — no need to open another menu
        echo ""
        echo -e "${CYAN}=== Panel credentials ===${NC}"
        show_panel_url
        # auto port: if 7777 was busy the binary picked the next free one
        _APORT=$(grep -o '"port": *[0-9]*' /etc/gre-panel/panel.json 2>/dev/null | grep -o '[0-9]*')
        if [[ -n "$_APORT" && "$_APORT" != "7777" ]]; then
            echo -e "${YELLOW}[!] Port 7777 was busy — panel auto-switched to ${_APORT} (saved, survives restarts).${NC}"
        fi
    else
        echo -e "${RED}[!] Panel failed to start — see: journalctl -u gre-panel${NC}"
        return 1
    fi
}

show_panel_url() {
    if [[ ! -f /etc/gre-panel/panel.json ]]; then
        echo -e "${YELLOW}Panel is not installed on this server (no /etc/gre-panel/panel.json). Run Setup first.${NC}"
        return 1
    fi
    local port base user
    port=$(grep -o '"port": *[0-9]*' /etc/gre-panel/panel.json 2>/dev/null | grep -o '[0-9]*')
    base=$(grep -o '"base_path": *"[^"]*"' /etc/gre-panel/panel.json 2>/dev/null | cut -d'"' -f4)
    user=$(grep -o '"username": *"[^"]*"' /etc/gre-panel/panel.json 2>/dev/null | cut -d'"' -f4)
    port=${port:-7777}
    user=${user:-admin}
    ensure_panel_pass
    MYIP=$(ip route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}')
    echo -e "${GREEN}Panel URL:  ${CYAN}http://${MYIP:-<this-server-ip>}:${port}/${base}${NC}"
    echo -e "${GREEN}Username:   ${CYAN}${user}${NC}"
    echo -e "${GREEN}Password:   ${CYAN}${PANEL_PASS}${NC}"
}

# make sure a plaintext password exists and load it into $PANEL_PASS.
# fresh installs already have it (binary writes it); old installs get a new one.
ensure_panel_pass() {
    PANEL_PASS=$(cat /etc/gre-panel/panel.pass 2>/dev/null)
    if [[ -n "$PANEL_PASS" ]]; then return 0; fi
    echo -e "${YELLOW}[*] No saved panel password — generating a new one...${NC}"
    local NEWPASS HASH
    NEWPASS=$(tr -dc '0-9' </dev/urandom | head -c 8)
    HASH=$(echo -n "$NEWPASS" | sha256sum | awk '{print $1}')
    if [[ -z "$HASH" ]] || ! command -v python3 >/dev/null 2>&1; then
        echo -e "${RED}[!] Cannot reset password (need sha256sum + python3). Change it from web Settings instead.${NC}"
        PANEL_PASS="(unknown — reset via web Settings)"
        return 1
    fi
    python3 - "$HASH" <<'PYEOF'
import json, sys
p = '/etc/gre-panel/panel.json'
d = json.load(open(p))
d['pass_hash'] = sys.argv[1]
json.dump(d, open(p, 'w'), indent=2)
PYEOF
    echo -n "$NEWPASS" > /etc/gre-panel/panel.pass
    chmod 600 /etc/gre-panel/panel.pass
    systemctl restart gre-panel 2>/dev/null
    sleep 2
    PANEL_PASS="$NEWPASS"
    return 0
}

# save plaintext panel password next to config (user chose convenience over max security)
save_panel_pass() {
    local pass="$1"
    [[ -n "$pass" ]] && echo -n "$pass" > /etc/gre-panel/panel.pass 2>/dev/null
    chmod 600 /etc/gre-panel/panel.pass 2>/dev/null || true
}

# ---- Watchdog & Scheduled Encrypted Backup ----
init_watchdog_json() {
    mkdir -p /etc/gre-panel
    if [[ ! -f "$WATCHDOG_FILE" ]]; then
        cat << 'EOF' > "$WATCHDOG_FILE"
{
  "enabled": false,
  "interval_sec": 60,
  "fail_threshold": 2,
  "tg_bot_token": "",
  "tg_chat_id": "",
  "tg_route": "direct",
  "tg_tunnel_port": 0,
  "backup_every_hours": 0,
  "backup_daily_at": "",
  "last_check": "",
  "consec_fails": 0,
  "last_alert": ""
}
EOF
        chmod 600 "$WATCHDOG_FILE" 2>/dev/null || true
    fi
}

watchdog_get_peer_gre() {
    local PEER=""
    if ip link show "$TUNNEL_NAME" >/dev/null 2>&1; then
        local INNER
        INNER=$(ip -4 addr show dev "$TUNNEL_NAME" 2>/dev/null | awk '/inet / {print $2}' | cut -d/ -f1 | head -n1)
        if [[ -n "$INNER" ]]; then
            if [[ "$INNER" == "$IRAN_GRE_IP" ]]; then
                PEER="$FOREIGN_GRE_IP"
            elif [[ "$INNER" == "$FOREIGN_GRE_IP" ]]; then
                PEER="$IRAN_GRE_IP"
            else
                local IFS=. read -r a b c d <<< "$INNER"
                if (( d % 2 == 0 )); then
                    PEER="$a.$b.$c.$((d - 1))"
                else
                    PEER="$a.$b.$c.$((d + 1))"
                fi
            fi
        fi
    fi
    if [[ -z "$PEER" && -f "$PEERS_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        PEER=$(python3 -c '
import json
try:
    with open("'"$PEERS_FILE"'") as f:
        d = json.load(f)
        peers = d.get("peers", [])
        if peers and "peer_gre" in peers[0]:
            print(peers[0]["peer_gre"])
except Exception:
    pass
' 2>/dev/null)
    fi
    echo "$PEER"
}

watchdog_check() {
    init_watchdog_json
    local PEER_GRE
    PEER_GRE=$(watchdog_get_peer_gre)
    local GRE_OK=0
    if [[ -n "$PEER_GRE" ]] && ping -c 1 -W 2 "$PEER_GRE" >/dev/null 2>&1; then
        GRE_OK=1
    fi

    local FRP_NAME=""
    local FRP_OK=0
    if [[ -f /etc/frp/frpc.toml ]] || systemctl list-unit-files 2>/dev/null | grep -q "^frpc\.service"; then
        FRP_NAME="frpc"
        systemctl is-active --quiet frpc 2>/dev/null && FRP_OK=1
    elif [[ -f /etc/frp/frps.toml ]] || systemctl list-unit-files 2>/dev/null | grep -q "^frps\.service"; then
        FRP_NAME="frps"
        systemctl is-active --quiet frps 2>/dev/null && FRP_OK=1
    else
        if systemctl list-units --type=service 2>/dev/null | grep -q 'frps'; then
            FRP_NAME="frps"
            FRP_OK=1
        fi
    fi

    local FAILS=0
    if [[ -f "$WATCHDOG_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        FAILS=$(python3 -c '
import json
try:
    with open("'"$WATCHDOG_FILE"'") as f:
        print(int(json.load(f).get("consec_fails", 0)))
except Exception:
    print(0)
' 2>/dev/null || echo 0)
    fi

    local STATUS="down"
    local DETAIL=""
    if [[ $GRE_OK -eq 1 && $FRP_OK -eq 1 ]]; then
        STATUS="up"
        DETAIL="GRE ping OK ($PEER_GRE), FRP $FRP_NAME active"
    else
        local ERR_PARTS=()
        if [[ $GRE_OK -ne 1 ]]; then
            if [[ -z "$PEER_GRE" ]]; then
                ERR_PARTS+=("GRE interface missing/down")
            else
                ERR_PARTS+=("GRE ping $PEER_GRE failed")
            fi
        fi
        if [[ $FRP_OK -ne 1 ]]; then
            ERR_PARTS+=("FRP ${FRP_NAME:-service} inactive")
        fi
        DETAIL=$(IFS="; "; echo "${ERR_PARTS[*]}")
    fi

    echo "WATCHDOG status=$STATUS fails=$FAILS detail=$DETAIL"
    return 0
}

watchdog_send() {
    local TEXT="$1"
    [[ -z "$TEXT" ]] && return 1
    init_watchdog_json

    local CFG
    CFG=$(python3 -c '
import json
try:
    with open("'"$WATCHDOG_FILE"'") as f:
        d = json.load(f)
        tok = d.get("tg_bot_token", "").strip()
        cid = str(d.get("tg_chat_id", "")).strip()
        route = d.get("tg_route", "direct").strip()
        port = str(d.get("tg_tunnel_port", 0)).strip()
        print(f"{tok}\t{cid}\t{route}\t{port}")
except Exception:
    pass
' 2>/dev/null)

    local TG_TOKEN TG_CHAT_ID TG_ROUTE TG_PORT
    IFS=$'\t' read -r TG_TOKEN TG_CHAT_ID TG_ROUTE TG_PORT <<< "$CFG"

    if [[ -z "$TG_TOKEN" || -z "$TG_CHAT_ID" ]]; then
        echo -e "${YELLOW}[!] Telegram bot token or chat ID not configured in ${WATCHDOG_FILE}.${NC}" >&2
        return 1
    fi

    local HOST
    HOST="$(hostname 2>/dev/null || echo 'server')"
    local FULL_MSG="[Hashem ${HOST}] ${TEXT}"

    local CURL_ARGS=(-sS -f)
    if [[ "$TG_ROUTE" == "tunnel" ]]; then
        if [[ -z "$TG_PORT" || "$TG_PORT" -le 0 ]]; then
            echo -e "${RED}[!] Telegram route is set to tunnel but tunnel port is not configured.${NC}" >&2
            return 1
        fi
        CURL_ARGS+=(--max-time 20 --socks5-hostname "127.0.0.1:${TG_PORT}")
    else
        CURL_ARGS+=(--max-time 15)
    fi

    local CURL_OUT
    CURL_OUT=$(curl "${CURL_ARGS[@]}" -d "chat_id=${TG_CHAT_ID}" --data-urlencode "text=${FULL_MSG}" "https://api.telegram.org/bot${TG_TOKEN}/sendMessage" 2>&1)
    local RET=$?

    if [[ $RET -ne 0 ]]; then
        local REDACTED_ERR
        REDACTED_ERR=$(echo "$CURL_OUT" | sed "s/${TG_TOKEN}/[REDACTED]/g")
        echo -e "${RED}[!] Telegram send failed: ${REDACTED_ERR}${NC}" >&2
        return 1
    fi
    return 0
}

watchdog_test() {
    echo -e "${CYAN}[*] Testing Telegram alerts...${NC}"
    if watchdog_send "✅ Hashem watchdog test OK"; then
        echo -e "${GREEN}[✔️] Telegram test message sent successfully.${NC}"
        return 0
    else
        echo -e "${RED}[!] Telegram test message failed. Check token, chat ID, and route.${NC}"
        return 1
    fi
}

restart_all_lite() {
    local u
    for u in /etc/systemd/system/gre-t*.service /etc/systemd/system/gre-tunnel.service /etc/systemd/system/frps*.service /etc/systemd/system/frpc.service; do
        [[ -f "$u" ]] || continue
        systemctl restart "$(basename "$u")" >/dev/null 2>&1
    done
}

watchdog_tick() {
    local LOCKFILE="/var/lock/hashem-watchdog.lock"
    mkdir -p /var/lock 2>/dev/null || true
    exec 200>"$LOCKFILE" 2>/dev/null || exec 200>/tmp/hashem-watchdog.lock
    if ! flock -n 200; then
        echo "watchdog_tick: another instance running, exiting"
        return 0
    fi

    init_watchdog_json

    local TICK_ACTION
    TICK_ACTION=$(python3 -c '
import json, time
try:
    with open("'"$WATCHDOG_FILE"'") as f:
        d = json.load(f)
    enabled = d.get("enabled", False)
    backup_every = int(d.get("backup_every_hours", 0))
    backup_daily = d.get("backup_daily_at", "").strip()
    last_backup = int(d.get("last_backup", 0))
    last_bdate = d.get("last_backup_date", "")
    now = int(time.time())
    do_backup = False
    if backup_every > 0:
        if (now - last_backup) >= (backup_every * 3600):
            do_backup = True
    elif backup_daily:
        cur_hm = time.strftime("%H:%M")
        cur_date = time.strftime("%Y-%m-%d")
        if cur_hm == backup_daily and last_bdate != cur_date:
            do_backup = True
    print(f"{enabled} {do_backup}")
except Exception as e:
    print("False False")
' 2>/dev/null)

    local IS_ENABLED="False"
    local DO_BACKUP="False"
    read -r IS_ENABLED DO_BACKUP <<< "$TICK_ACTION"

    if [[ "$IS_ENABLED" == "True" || "$IS_ENABLED" == "true" ]]; then
        local CHECK_OUT
        CHECK_OUT=$(watchdog_check)
        local STATUS DETAIL
        STATUS=$(echo "$CHECK_OUT" | sed -n 's/.*status=\([^ ]*\).*/\1/p')
        DETAIL=$(echo "$CHECK_OUT" | sed -n 's/.*detail=\(.*\)/\1/p')

        local DECISION
        DECISION=$(CHECK_STATUS="$STATUS" CHECK_DETAIL="$DETAIL" python3 -c '
import json, os, time

path = "'"$WATCHDOG_FILE"'"
st = os.environ.get("CHECK_STATUS", "down")
detail = os.environ.get("CHECK_DETAIL", "")
now = int(time.time())
now_str = time.strftime("%Y-%m-%d %H:%M:%S")

try:
    with open(path) as f:
        d = json.load(f)
except Exception:
    d = {"enabled": True, "fail_threshold": 2, "consec_fails": 0, "last_alert": ""}

threshold = int(d.get("fail_threshold", 2))
consec = int(d.get("consec_fails", 0))
last_alert = d.get("last_alert", "")
down_since = int(d.get("down_since", 0))

action = "NONE"

if st == "up":
    if last_alert == "down":
        down_min = max(1, int((now - down_since + 59) / 60))
        action = f"RECOVERED {down_min}"
        d["last_alert"] = "up"
        d["down_since"] = 0
    d["consec_fails"] = 0
else:
    consec += 1
    d["consec_fails"] = consec
    if consec >= threshold and last_alert != "down":
        action = "DOWN"
        d["last_alert"] = "down"
        d["down_since"] = now

d["last_check"] = now_str

tmp = path + ".tmp"
with open(tmp, "w") as f:
    json.dump(d, f, indent=2)
os.replace(tmp, path)
os.chmod(path, 0o600)
print(action)
' 2>/dev/null)

        if [[ "$DECISION" == DOWN* ]]; then
            watchdog_send "🔴 Tunnel DOWN: ${DETAIL} (attempting tunnel restart)" || true
            restart_all_lite
        elif [[ "$DECISION" == RECOVERED* ]]; then
            local DMIN
            DMIN=$(echo "$DECISION" | awk '{print $2}')
            watchdog_send "🟢 Tunnel RECOVERED (was down ${DMIN}m)" || true
        fi
    fi

    if [[ "$DO_BACKUP" == "True" || "$DO_BACKUP" == "true" ]]; then
        backup_now >/dev/null 2>&1 || true
        python3 -c '
import json, time
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["last_backup"] = int(time.time())
    d["last_backup_date"] = time.strftime("%Y-%m-%d")
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null || true
    fi

    return 0
}

backup_now() {
    local OUTDIR="$BACKUP_DIR"
    local KEEP=7
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --keep) KEEP="$2"; shift 2 ;;
            *)
                if [[ "$1" != --* ]]; then
                    OUTDIR="$1"
                fi
                shift
                ;;
        esac
    done

    mkdir -p "$OUTDIR"
    chmod 700 "$OUTDIR" 2>/dev/null || true

    if [[ ! -f /etc/gre-panel/panel.pass ]]; then
        echo -e "${RED}[!] /etc/gre-panel/panel.pass not found — cannot encrypt backup.${NC}" >&2
        return 1
    fi

    local DATE_STR
    DATE_STR=$(date +%Y%m%d-%H%M%S)
    local OUT_FILE="${OUTDIR}/hashem-backup-${DATE_STR}.enc"

    local FILES=()
    local f
    for f in /etc/frp/*.toml /etc/gre-panel/panel.json /etc/gre-panel/peers.json /etc/gre-panel/watchdog.json \
             /etc/gre-panel/perf.json \
             /etc/systemd/system/gre-*.service /etc/systemd/system/frps*.service \
             /etc/systemd/system/frpc*.service /etc/systemd/system/gre-chaff*.service; do
        [[ -f "$f" ]] && FILES+=("$f")
    done

    if [[ ${#FILES[@]} -eq 0 ]]; then
        echo -e "${RED}[!] No configuration or unit files found to back up.${NC}" >&2
        return 1
    fi

    if ! tar -czf - "${FILES[@]}" 2>/dev/null | openssl enc -aes-256-cbc -pbkdf2 -pass file:/etc/gre-panel/panel.pass -out "$OUT_FILE"; then
        echo -e "${RED}[!] Failed to create encrypted backup.${NC}" >&2
        rm -f "$OUT_FILE"
        return 1
    fi

    chmod 600 "$OUT_FILE" 2>/dev/null || true
    local SIZE
    SIZE=$(stat -c%s "$OUT_FILE" 2>/dev/null || echo 0)
    local HSIZE
    HSIZE=$(du -h "$OUT_FILE" 2>/dev/null | cut -f1)

    echo "BACKUP path=${OUT_FILE} size=${SIZE}"
    echo -e "${GREEN}[✔️] Backup created: ${OUT_FILE} (${HSIZE})${NC}"

    if [[ "$KEEP" -gt 0 ]]; then
        local OLD_FILES
        OLD_FILES=$(ls -1t "$OUTDIR"/hashem-backup-*.enc 2>/dev/null | tail -n +$((KEEP + 1)))
        if [[ -n "$OLD_FILES" ]]; then
            echo "$OLD_FILES" | xargs -r rm -f
            echo -e "${CYAN}[*] Pruned old backups (kept latest ${KEEP}).${NC}"
        fi
    fi
    return 0
}

backup_restore() {
    local FILE=""
    local DRY_RUN=0
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --dry-run) DRY_RUN=1; shift ;;
            *) FILE="$1"; shift ;;
        esac
    done

    if [[ -z "$FILE" || ! -f "$FILE" ]]; then
        echo -e "${RED}[!] Backup file not found: '${FILE}'${NC}" >&2
        return 1
    fi
    if [[ ! -f /etc/gre-panel/panel.pass ]]; then
        echo -e "${RED}[!] /etc/gre-panel/panel.pass not found — cannot decrypt backup.${NC}" >&2
        return 1
    fi

    local TMP_D
    TMP_D=$(mktemp -d)
    trap 'rm -rf "$TMP_D"' RETURN

    echo -e "${CYAN}[*] Decrypting backup archive with panel password...${NC}"
    if ! openssl enc -d -aes-256-cbc -pbkdf2 -pass file:/etc/gre-panel/panel.pass -in "$FILE" -out "$TMP_D/backup.tar.gz" 2>/dev/null; then
        echo -e "${RED}[!] Decryption failed: invalid panel password or file corrupted.${NC}" >&2
        return 1
    fi

    echo -e "${CYAN}[*] Verifying archive contents...${NC}"
    if ! tar -ztf "$TMP_D/backup.tar.gz" >"$TMP_D/list.txt" 2>/dev/null; then
        echo -e "${RED}[!] Archive verification failed: invalid tar archive.${NC}" >&2
        return 1
    fi

    if [[ "$DRY_RUN" -eq 1 ]]; then
        echo -e "${GREEN}[✔️] Archive verified OK. Files inside:${NC}"
        cat "$TMP_D/list.txt"
        return 0
    fi

    echo -e "${CYAN}[*] Restoring configuration files and systemd units...${NC}"
    tar -xzf "$TMP_D/backup.tar.gz" -C /
    chmod 600 /etc/gre-panel/*.json 2>/dev/null || true
    chmod 600 /etc/gre-panel/*.pass 2>/dev/null || true
    echo -e "${GREEN}[✔️] Files restored:${NC}"
    cat "$TMP_D/list.txt"

    echo -e "${CYAN}[*] Reloading systemd daemon...${NC}"
    systemctl daemon-reload

    echo -e "${CYAN}[*] Restarting tunnel services...${NC}"
    restart_all

    echo -e "${GREEN}[✔️] Restore completed successfully.${NC}"
    return 0
}

install_watchdog_units() {
    [[ -x "$HASHEM_BIN" ]] || { cp "$0" "$HASHEM_BIN" 2>/dev/null && chmod +x "$HASHEM_BIN"; } || true
    cat << 'EOF' > /etc/systemd/system/hashem-watchdog.service
[Unit]
Description=Hashem Watchdog and Scheduled Backup Tick
After=network.target

[Service]
Type=oneshot
ExecStart=/usr/local/bin/hashem watchdog tick
EOF

    cat << 'EOF' > /etc/systemd/system/hashem-watchdog.timer
[Unit]
Description=Run Hashem Watchdog every minute
After=network.target

[Timer]
OnBootSec=1min
OnUnitActiveSec=1min
Persistent=true

[Install]
WantedBy=timers.target
EOF

    systemctl daemon-reload
}

watchdog_on() {
    init_watchdog_json
    install_watchdog_units
    systemctl enable --now hashem-watchdog.timer >/dev/null 2>&1
    python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["enabled"] = True
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null || true
    echo -e "${GREEN}[✔️] Watchdog enabled (systemd timer active, checks every 1 min).${NC}"
}

watchdog_off() {
    init_watchdog_json
    systemctl stop hashem-watchdog.timer hashem-watchdog.service >/dev/null 2>&1 || true
    systemctl disable hashem-watchdog.timer >/dev/null 2>&1 || true
    python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["enabled"] = False
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null || true
    echo -e "${YELLOW}[*] Watchdog disabled (systemd timer stopped).${NC}"
}

watchdog_status_full() {
    init_watchdog_json
    echo -e "${CYAN}==========================================================${NC}"
    echo -e "${CYAN}                 Hashem Watchdog Status                   ${NC}"
    echo -e "${CYAN}==========================================================${NC}"

    local INFO
    INFO=$(python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    en = "Enabled" if d.get("enabled", False) else "Disabled"
    tok = d.get("tg_bot_token", "").strip()
    if tok:
        masked = tok[:6] + "..." + tok[-4:] if len(tok) > 10 else "******"
    else:
        masked = "(not configured)"
    cid = str(d.get("tg_chat_id", "")) or "(not configured)"
    route = d.get("tg_route", "direct")
    port = str(d.get("tg_tunnel_port", 0))
    fails = str(d.get("consec_fails", 0))
    thresh = str(d.get("fail_threshold", 2))
    last_c = d.get("last_check", "") or "(none yet)"
    last_a = d.get("last_alert", "") or "(none)"
    be = int(d.get("backup_every_hours", 0))
    bd = d.get("backup_daily_at", "")
    if be > 0:
        sched = f"Every {be} hours"
    elif bd:
        sched = f"Daily at {bd}"
    else:
        sched = "Disabled"
    print(f"{en}\t{masked}\t{cid}\t{route}\t{port}\t{fails}\t{thresh}\t{last_c}\t{last_a}\t{sched}")
except Exception as e:
    print(f"Error\t-\t-\t-\t-\t0\t2\t-\t-\tDisabled")
' 2>/dev/null)

    local EN TOK CID ROUTE PORT FAILS THRESH LAST_C LAST_A SCHED
    IFS=$'\t' read -r EN TOK CID ROUTE PORT FAILS THRESH LAST_C LAST_A SCHED <<< "$INFO"

    local TIMER_ACTIVE="inactive"
    if systemctl is-active --quiet hashem-watchdog.timer 2>/dev/null; then
        TIMER_ACTIVE="active (every 1 min)"
    fi

    echo -e "Watchdog State:     ${CYAN}${EN}${NC} (systemd timer: ${TIMER_ACTIVE})"
    echo -e "Consecutive Fails:  ${FAILS} / ${THRESH}"
    echo -e "Last Check:         ${LAST_C}"
    echo -e "Last Alert:         ${LAST_A}"
    echo ""
    echo -e "${YELLOW}── Telegram Alerts ──${NC}"
    echo -e "Bot Token:          ${TOK}"
    echo -e "Chat ID:            ${CID}"
    if [[ "$ROUTE" == "tunnel" ]]; then
        echo -e "Route:              tunnel (SOCKS5 127.0.0.1:${PORT})"
    else
        echo -e "Route:              direct"
    fi
    echo ""
    echo -e "${YELLOW}── Backup Schedule & Files ──${NC}"
    echo -e "Schedule:           ${SCHED}"
    local BC=0
    if [[ -d "$BACKUP_DIR" ]]; then
        BC=$(ls -1 "$BACKUP_DIR"/hashem-backup-*.enc 2>/dev/null | wc -l)
    fi
    echo -e "Stored Backups:     ${BC} in ${BACKUP_DIR}"
    if [[ "$BC" -gt 0 ]]; then
        ls -lh "$BACKUP_DIR"/hashem-backup-*.enc 2>/dev/null | awk '{print "  " $9 " (" $5 ", " $6 " " $7 " " $8 ")"}' | tail -n 5
    fi
    echo ""
    echo -e "${YELLOW}── Live Health Check ──${NC}"
    watchdog_check
    echo -e "${CYAN}==========================================================${NC}"
}

find_live_proxy_ports() {
    local PORTS=()
    if [[ -f /etc/frp/frpc.toml ]]; then
        while read -r p; do
            [[ -n "$p" ]] && PORTS+=("$p")
        done < <(grep -E '^(remotePort|localPort)\s*=' /etc/frp/frpc.toml 2>/dev/null | awk -F= '{print $2}' | tr -d ' "')
    fi
    local f
    for f in /etc/frp/frps*.toml; do
        [[ -f "$f" ]] || continue
        while read -r p; do
            [[ -n "$p" ]] && PORTS+=("$p")
        done < <(grep -E '^(remotePort|localPort)\s*=' "$f" 2>/dev/null | awk -F= '{print $2}' | tr -d ' "')
    done
    if [[ -f "$PEERS_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        while read -r p; do
            [[ -n "$p" ]] && PORTS+=("$p")
        done < <(python3 -c '
import json
try:
    with open("'"$PEERS_FILE"'") as f:
        d = json.load(f)
        for peer in d.get("peers", []):
            for port in peer.get("ports", []):
                print(port)
except Exception:
    pass
' 2>/dev/null)
    fi
    if [[ ${#PORTS[@]} -gt 0 ]]; then
        printf "%s\n" "${PORTS[@]}" | sort -n -u
    fi
}

menu_watchdog() {
    while true; do
        clear
        echo -e "${CYAN}==========================================================${NC}"
        echo -e "${CYAN}              Watchdog & Encrypted Backup                 ${NC}"
        echo -e "${CYAN}==========================================================${NC}"
        echo ""
        init_watchdog_json
        local W_EN
        W_EN=$(python3 -c '
import json
try:
    with open("'"$WATCHDOG_FILE"'") as f:
        print("ENABLED" if json.load(f).get("enabled", False) else "DISABLED")
except Exception:
    print("DISABLED")
' 2>/dev/null)
        if [[ "$W_EN" == "ENABLED" ]]; then
            echo -e "Watchdog Status: ${GREEN}● ENABLED${NC} (checks every 1 min)"
        else
            echo -e "Watchdog Status: ${RED}○ DISABLED${NC}"
        fi
        echo ""
        echo "  1) Enable / Disable Watchdog"
        echo "  2) Set Telegram (Bot Token & Chat ID)"
        echo "  3) Test Telegram Alert"
        echo "  4) Route Direct vs Tunnel (+pick tunnel socks port)"
        echo "  5) Backup Now (OpenSSL AES-256-CBC Encrypted)"
        echo "  6) Schedule Backup (Every-N-Hours OR Daily at HH:MM)"
        echo "  7) Restore from Encrypted Backup"
        echo "  8) View Full Status & Stored Backups"
        echo "  0) Back to Main Menu"
        echo ""
        read -p "Select an option [0-8]: " SUBOPT
        case "$SUBOPT" in
            1)
                if [[ "$W_EN" == "ENABLED" ]]; then
                    watchdog_off
                else
                    watchdog_on
                fi
                read -p "Press Enter to continue..." _
                ;;
            2)
                echo -e "\n${CYAN}── Configure Telegram Alerts ──${NC}"
                read -p "Enter Telegram Bot Token: " INPUT_TOKEN
                read -p "Enter Telegram Chat ID: " INPUT_CID
                if [[ -n "$INPUT_TOKEN" || -n "$INPUT_CID" ]]; then
                    python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
tok = "'"$INPUT_TOKEN"'".strip()
cid = "'"$INPUT_CID"'".strip()
try:
    with open(path) as f:
        d = json.load(f)
    if tok:
        d["tg_bot_token"] = tok
    if cid:
        d["tg_chat_id"] = cid
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception as e:
    print(e)
' 2>/dev/null
                    echo -e "${GREEN}[✔️] Telegram settings saved.${NC}"
                else
                    echo -e "${YELLOW}[*] No changes made.${NC}"
                fi
                read -p "Press Enter to continue..." _
                ;;
            3)
                watchdog_test
                read -p "Press Enter to continue..." _
                ;;
            4)
                echo -e "\n${CYAN}── Telegram Delivery Route ──${NC}"
                echo "  1) Direct (curl direct to Telegram API)"
                echo "  2) Via Tunnel (SOCKS5 through tunnel port)"
                read -p "Choose route [1-2]: " ROUTE_CHOICE
                if [[ "$ROUTE_CHOICE" == "1" ]]; then
                    python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["tg_route"] = "direct"
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null
                    echo -e "${GREEN}[✔️] Route set to Direct.${NC}"
                elif [[ "$ROUTE_CHOICE" == "2" ]]; then
                    local PORTS=()
                    mapfile -t PORTS < <(find_live_proxy_ports)
                    local CHOSEN_PORT=0
                    if [[ ${#PORTS[@]} -gt 0 ]]; then
                        echo -e "\nDetected live tunnel ports:"
                        local idx=1
                        for p in "${PORTS[@]}"; do
                            echo "  $idx) Port $p"
                            ((idx++))
                        done
                        echo "  $idx) Enter custom port manually"
                        read -p "Select port [1-$idx]: " PIDX
                        if [[ "$PIDX" =~ ^[0-9]+$ ]] && (( PIDX >= 1 && PIDX < idx )); then
                            CHOSEN_PORT="${PORTS[$((PIDX-1))]}"
                        else
                            read -p "Enter SOCKS5 tunnel port (1-65535): " CHOSEN_PORT
                        fi
                    else
                        read -p "Enter SOCKS5 tunnel port (1-65535): " CHOSEN_PORT
                    fi
                    if is_valid_port "$CHOSEN_PORT"; then
                        python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["tg_route"] = "tunnel"
    d["tg_tunnel_port"] = int("'"$CHOSEN_PORT"'")
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null
                        echo -e "${GREEN}[✔️] Route set to Tunnel (127.0.0.1:${CHOSEN_PORT}).${NC}"
                    else
                        echo -e "${RED}[!] Invalid port number.${NC}"
                    fi
                fi
                read -p "Press Enter to continue..." _
                ;;
            5)
                echo -e "\n${CYAN}── Creating Encrypted Backup ──${NC}"
                backup_now
                read -p "Press Enter to continue..." _
                ;;
            6)
                echo -e "\n${CYAN}── Schedule Encrypted Backup ──${NC}"
                echo "  1) Every N hours"
                echo "  2) Daily at fixed time (HH:MM)"
                echo "  3) Disable scheduled backups"
                read -p "Select schedule mode [1-3]: " S_CHOICE
                case "$S_CHOICE" in
                    1)
                        read -p "Enter interval in hours (e.g. 6): " N_HOURS
                        if [[ "$N_HOURS" =~ ^[0-9]+$ ]] && (( N_HOURS >= 1 && N_HOURS <= 168 )); then
                            python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["backup_every_hours"] = int("'"$N_HOURS"'")
    d["backup_daily_at"] = ""
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null
                            echo -e "${GREEN}[✔️] Backup scheduled every ${N_HOURS} hours.${NC}"
                        else
                            echo -e "${RED}[!] Invalid hours (must be 1-168).${NC}"
                        fi
                        ;;
                    2)
                        read -p "Enter daily time in 24h format HH:MM (e.g. 03:00): " DAILY_T
                        if [[ "$DAILY_T" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]]; then
                            python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["backup_every_hours"] = 0
    d["backup_daily_at"] = "'"$DAILY_T"'"
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null
                            echo -e "${GREEN}[✔️] Backup scheduled daily at ${DAILY_T}.${NC}"
                        else
                            echo -e "${RED}[!] Invalid time format (use HH:MM e.g. 03:00).${NC}"
                        fi
                        ;;
                    3)
                        python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["backup_every_hours"] = 0
    d["backup_daily_at"] = ""
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null
                        echo -e "${GREEN}[✔️] Scheduled backups disabled.${NC}"
                        ;;
                    *)
                        echo -e "${RED}[!] Invalid option.${NC}"
                        ;;
                esac
                read -p "Press Enter to continue..." _
                ;;
            7)
                echo -e "\n${CYAN}── Restore Backup ──${NC}"
                local BAKS=()
                if [[ -d "$BACKUP_DIR" ]]; then
                    mapfile -t BAKS < <(ls -1t "$BACKUP_DIR"/hashem-backup-*.enc 2>/dev/null)
                fi
                if [[ ${#BAKS[@]} -eq 0 ]]; then
                    echo -e "${YELLOW}[!] No backups found in ${BACKUP_DIR}.${NC}"
                    read -p "Enter full path to backup file manually (or Enter to cancel): " MAN_FILE
                    if [[ -n "$MAN_FILE" ]]; then
                        backup_restore "$MAN_FILE"
                    fi
                else
                    echo "Available backups:"
                    local bidx=1
                    for b in "${BAKS[@]}"; do
                        local bsz
                        bsz=$(du -h "$b" 2>/dev/null | cut -f1)
                        echo "  $bidx) $(basename "$b") ($bsz)"
                        ((bidx++))
                    done
                    read -p "Select backup to restore [1-$((bidx-1))]: " PICK_B
                    if [[ "$PICK_B" =~ ^[0-9]+$ ]] && (( PICK_B >= 1 && PICK_B < bidx )); then
                        local SELECTED="${BAKS[$((PICK_B-1))]}"
                        read -p "Restore $(basename "$SELECTED")? Current configs will be overwritten and services restarted. (y/N): " CONFIRM_R
                        if [[ "$CONFIRM_R" =~ ^[Yy]$ ]]; then
                            backup_restore "$SELECTED"
                        else
                            echo -e "${YELLOW}[*] Restore cancelled.${NC}"
                        fi
                    else
                        echo -e "${RED}[!] Invalid choice.${NC}"
                    fi
                fi
                read -p "Press Enter to continue..." _
                ;;
            8)
                watchdog_status_full
                read -p "Press Enter to continue..." _
                ;;
            0)
                return 0
                ;;
            *)
                echo -e "${RED}[!] Invalid option.${NC}"
                sleep 1
                ;;
        esac
    done
}

cli_watchdog() {
    local SUB="$1"
    shift || true
    case "$SUB" in
        on) watchdog_on ;;
        off) watchdog_off ;;
        status) watchdog_status_full ;;
        test) watchdog_test ;;
        tick) watchdog_tick ;;
        check) watchdog_check ;;
        *) echo -e "${RED}[!] Unknown watchdog command: '$SUB' (want on|off|status|test|tick)${NC}"; return 1 ;;
    esac
}

cli_backup() {
    local SUB="$1"
    shift || true
    case "$SUB" in
        now) backup_now "$@" ;;
        restore) backup_restore "$@" ;;
        status)
            echo -e "${CYAN}=== Hashem Backups (${BACKUP_DIR}) ===${NC}"
            if [[ -d "$BACKUP_DIR" ]]; then
                ls -lh "$BACKUP_DIR"/hashem-backup-*.enc 2>/dev/null || echo "(no backups found)"
            else
                echo "(no backup directory)"
            fi
            ;;
        schedule)
            local MODE="" HOURS=0 DAILY=""
            while [[ $# -gt 0 ]]; do
                case "$1" in
                    --every|every) HOURS="$2"; MODE="interval"; shift 2 ;;
                    --daily|daily) DAILY="$2"; MODE="daily"; shift 2 ;;
                    --off|off) MODE="off"; shift ;;
                    *) shift ;;
                esac
            done
            init_watchdog_json
            if [[ "$MODE" == "interval" ]]; then
                if [[ "$HOURS" =~ ^[0-9]+$ ]] && (( HOURS >= 1 && HOURS <= 168 )); then
                    python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["backup_every_hours"] = int("'"$HOURS"'")
    d["backup_daily_at"] = ""
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null
                    echo -e "${GREEN}[✔️] Backup scheduled every ${HOURS} hours.${NC}"
                else
                    echo -e "${RED}[!] Invalid interval hours: '$HOURS' (1-168)${NC}"; return 1
                fi
            elif [[ "$MODE" == "daily" ]]; then
                if [[ "$DAILY" =~ ^([01][0-9]|2[0-3]):[0-5][0-9]$ ]]; then
                    python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["backup_every_hours"] = 0
    d["backup_daily_at"] = "'"$DAILY"'"
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null
                    echo -e "${GREEN}[✔️] Backup scheduled daily at ${DAILY}.${NC}"
                else
                    echo -e "${RED}[!] Invalid daily time format: '$DAILY' (HH:MM e.g. 03:00)${NC}"; return 1
                fi
            elif [[ "$MODE" == "off" ]]; then
                python3 -c '
import json
path = "'"$WATCHDOG_FILE"'"
try:
    with open(path) as f:
        d = json.load(f)
    d["backup_every_hours"] = 0
    d["backup_daily_at"] = ""
    with open(path + ".tmp", "w") as f:
        json.dump(d, f, indent=2)
    import os
    os.replace(path + ".tmp", path)
    os.chmod(path, 0o600)
except Exception:
    pass
' 2>/dev/null
                echo -e "${GREEN}[✔️] Scheduled backups disabled.${NC}"
            else
                echo -e "${RED}[!] Usage: hashem backup schedule [--every N | --daily HH:MM | --off]${NC}"; return 1
            fi
            ;;
        *)
            echo -e "${RED}[!] Unknown backup command: '$SUB' (want now|restore|schedule|status)${NC}"
            return 1
            ;;
    esac
}

update_all() {
    echo -e "${CYAN}[*] Updating Hashem (script + panel binary)...${NC}"
    TMP_U="$(mktemp -d)"
    trap 'rm -rf "$TMP_U"' RETURN
    # 1. fresh script from main
    if ! curl -fsSL --max-time 30 "${HASHEM_URL_BASE}/hashem.sh" -o "$TMP_U/hashem.sh"; then
        echo -e "${RED}[!] Failed to download latest hashem.sh — nothing changed.${NC}"
        return 1
    fi
    bash -n "$TMP_U/hashem.sh" || { echo -e "${RED}[!] Downloaded script failed syntax check — nothing changed.${NC}"; return 1; }
    if cmp -s "$TMP_U/hashem.sh" "$0" 2>/dev/null || cmp -s "$TMP_U/hashem.sh" ./hashem.sh 2>/dev/null; then
        echo -e "${GREEN}[✔️] hashem.sh is already the latest version.${NC}"
    else
        echo -e "${GREEN}[✔️] New hashem.sh downloaded and syntax-checked.${NC}"
    fi
    # 2. reinstall panel binary from latest release (downloads prebuilt, restarts service)
    echo -e "${CYAN}[*] Updating panel binary...${NC}"
    # backup panel config so a failed update can be rolled back
    PANEL_BAK=""
    if [[ -f /etc/gre-panel/panel.json ]]; then
        PANEL_BAK="$(mktemp -d)"
        cp -a /etc/gre-panel/panel.json /etc/gre-panel/panel.pass "$PANEL_BAK/" 2>/dev/null || true
    fi
    if ! install_panel; then
        echo -e "${RED}[!] Panel update failed — restoring previous config.${NC}"
        [[ -n "$PANEL_BAK" ]] && cp -a "$PANEL_BAK/panel.json" "$PANEL_BAK/panel.pass" /etc/gre-panel/ 2>/dev/null || true
        systemctl restart gre-panel 2>/dev/null || true
        return 1
    fi
    [[ -n "$PANEL_BAK" ]] && rm -rf "$PANEL_BAK"
    # 3. replace running script only after everything succeeded
    cp "$TMP_U/hashem.sh" "$0" 2>/dev/null || cp "$TMP_U/hashem.sh" ./hashem.sh
    chmod +x "$0" 2>/dev/null || true
    # 4. sync copies next to the panel binary + the hashem command + legacy
    # gre.sh symlink, so the web panel + grepanel always shell out to the
    # latest tune/setup logic (single source of truth)
    cp "$TMP_U/hashem.sh" "$HASHEM_SCRIPT" 2>/dev/null && chmod +x "$HASHEM_SCRIPT" || true
    cp "$TMP_U/hashem.sh" "$HASHEM_BIN" 2>/dev/null && chmod +x "$HASHEM_BIN" || true
    ln -sf "$HASHEM_SCRIPT" /usr/local/bin/gre.sh 2>/dev/null || true
    install_chaff_script || true
    rm -f /usr/local/bin/gre-chaff.sh 2>/dev/null || true
    update_chaff_existing_tunnels || true
    if [[ -f "$WATCHDOG_FILE" ]] && command -v python3 >/dev/null 2>&1; then
        local WD_EN
        WD_EN=$(python3 -c '
import json
try:
    with open("'"$WATCHDOG_FILE"'") as f:
        print(json.load(f).get("enabled", False))
except Exception:
    print(False)
' 2>/dev/null)
        if [[ "$WD_EN" == "True" || "$WD_EN" == "true" ]]; then
            install_watchdog_units
            systemctl enable --now hashem-watchdog.timer >/dev/null 2>&1 || true
        fi
    fi
    PANEL_VER=$("$PANEL_BIN" --version 2>/dev/null || echo "unknown")
    echo -e "${GREEN}[✔️] Update complete — script + panel are latest (panel: ${PANEL_VER}). Re-run the script to use the new menu.${NC}"
}

main_menu() {
    clear
    echo -e "${CYAN}"
    echo "=========================================================="
    echo "       GRE + FRP Reverse Tunnel Manager (Iran <-> Kharej)"
    echo "     Layer 3 GRE Tunnel + Encrypted TLS FRP Reverse Relay"
    echo "=========================================================="
    echo -e "${NC}"
    echo -e "${YELLOW}── Setup ──${NC}"
    echo "  1) Setup IRAN Server    (GRE + FRP Server / frps)"
    echo "  2) Setup FOREIGN Server (GRE + FRP Client / frpc Reverse)"
    echo ""
    echo -e "${YELLOW}── Peer Tunnels (Iran: more foreign servers) ──${NC}"
    echo "  3) Add Peer Tunnel"
    echo "  4) List Peer Tunnels"
    echo "  5) Remove Peer Tunnel"
    echo ""
    echo -e "${YELLOW}── Monitor & Control ──${NC}"
    echo "  6) Check Connection Status & GRE Ping Test"
    echo "  7) View FRP Live Logs"
    echo "  8) Restart Tunnel Services"
    echo "  9) Remove Tunnel (GRE + FRP, panel stays)"
    echo ""
    echo -e "${YELLOW}── Tune ──${NC}"
    echo " 10) Optimize Tunnel (BBR + buffers + MTU/MSS, with backup)"
    echo " 11) Restore Pre-Optimize Settings"
    echo " 12) Optimization Status"
    echo " 19) Traffic Chaff / Obfuscation (idle-gap filler: on/off/status)"
    echo " 20) Watchdog & Backup (Telegram alerts, route direct/tunnel, encrypted backup)"
    echo " 21) DPI Shield (rate-limit reverse ports against flood: on/off/status)"
    echo " 22) Performance & Obfuscation Toggles (proxy crypto/comp, forced TLS, DPI rate)"
    echo ""
    echo -e "${YELLOW}── Panel & System ──${NC}"
    echo " 13) Show Panel URL + Username + Password"
    echo " 14) Panel HTTPS (Let's Encrypt certificate)"
    echo " 15) Update All (latest script + latest panel binary)"
    echo " 16) Free RAM (journald cap 16M + drop cache + 1GB swap)"
    echo " 17) hashem CLI help (non-interactive commands)"
    echo " 18) Uninstall Everything (tunnel + panel + 'hashem' command)"
    echo "  0) Exit"
    echo ""
    read -p "Select an option [0-22]: " OPTION

    case "$OPTION" in
        1)
            setup_iran_server
            ;;
        2)
            setup_foreign_server
            ;;
        3)
            menu_add_peer
            ;;
        4)
            peer_list_pretty
            ;;
        5)
            menu_remove_peer
            ;;
        6)
            check_status
            ;;
        7)
            show_logs
            ;;
        8)
            restart_all
            ;;
        9)
            remove_tunnel
            ;;
        10)
            tune_apply
            ;;
        11)
            tune_restore
            ;;
        12)
            tune_status
            ;;
        13)
            show_panel_url
            ;;
        14)
            panel_tls_issue
            ;;
        15)
            update_all
            ;;
        16)
            free_ram
            ;;
        17)
            usage_cli
            ;;
        18)
            uninstall_all
            ;;
        19)
            menu_chaff
            ;;
        20)
            menu_watchdog
            ;;
        21)
            menu_dpi_shield
            ;;
        22)
            menu_perf
            ;;
        0)
            echo "Exiting..."
            exit 0
            ;;
        *)
            echo -e "${RED}[!] Invalid option.${NC}"
            ;;
    esac
}

check_root
# Non-interactive CLI: hashem.sh setup-iran|setup-foreign with flags.
# The setup_*_noninteractive + _setup_foreign_full functions above are the
# SINGLE source of truth — menu, CLI, and web panel all run the same steps.
usage_cli() {
    cat <<EOF
Usage:
  hashem                                    # interactive menu (options 0-22)
  hashem setup-iran    --local-pub IP --remote-pub IP [--frp-port N] [--local-gre IP] [--peer-gre IP] [--token T] [--chaff low|mid|off] [--force]
  hashem setup-foreign --local-pub IP --remote-pub IP [--frp-port N] --token T --ports "443, 2083" [--local-gre IP] [--peer-gre IP] [--chaff low|mid|off] [--force]
                       # ... or: hashem setup-foreign --bundle hsh1_...  (fills everything; explicit flags win)
  hashem status | remove-tunnel [--force] | show-panel-url
  hashem uninstall [--force]                   # full wipe: tunnel + panel + 'hashem' itself
  hashem add-peer --local-pub IP --remote-pub IP [--frp-port N] --token T --local-gre IP --peer-gre IP --ports "443, 2083" [--name LABEL] [--bundle hsh1_...] [--chaff low|mid|off]
  hashem remove-peer --id N [--force] | peer-list | peer-token --id N
  hashem logs | restart | panel-tls [domain] [email]   # (also: bash hashem.sh ...)
  hashem optimize | restore | tune-status
  hashem perf status|enc on|off|comp on|off|tls on|off|chaff off|low|mid|dpi on|off|apply
  hashem chaff on|off|status                   # traffic obfuscation (idle-gap filler)
  hashem dpi-shield on|off|status              # rate-limit reverse ports against DPI flood
  hashem watchdog on|off|status|test|tick      # tunnel watchdog monitoring & alerts
  hashem backup now [--keep N] | restore <f> | schedule ... | status
  hashem tgsend "msg"                          # send Telegram alert manually
  hashem update | update-all                   # update script + panel to latest release
  hashem free-ram                              # cap journald + drop cache + 1GB swap

Setup bundle (one string with everything foreign needs):
  hsh1_<IRAN_PUB>_<FRP_PORT>_<IRAN_GRE>_<FOREIGN_GRE>_<TOKEN>[_<PORTS>]
  e.g. hsh1_85.1.2.3_34567_10.10.10.2_10.10.10.1_AbCdEf1234567890AbCdEf1234567890_443-2083
  Printed as BUNDLE:... by setup-iran / add-peer / peer-token; paste it as
  --bundle (CLI), the token prompt (menu), or the Foreign token field (panel).
EOF
}

cli_setup_iran() {
    local LOCAL_PUB="" REMOTE_PUB="" FRP_PORT="" LOCAL_GRE="$IRAN_GRE_IP" PEER_GRE="$FOREIGN_GRE_IP" TOKEN="" FORCE=0
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --local-pub) LOCAL_PUB="$2"; shift 2 ;;
            --remote-pub) REMOTE_PUB="$2"; shift 2 ;;
            --frp-port) FRP_PORT="$2"; shift 2 ;;
            --local-gre) LOCAL_GRE="$2"; shift 2 ;;
            --peer-gre) PEER_GRE="$2"; shift 2 ;;
            --token) TOKEN="$2"; shift 2 ;;
            --chaff) CHAFF_PROFILE="$2"; shift 2 ;;
            --force) FORCE=1; shift ;;
            -h|--help) usage_cli; return 0 ;;
            *) echo -e "${RED}[!] Unknown flag: $1${NC}"; usage_cli; return 1 ;;
        esac
    done
    CHAFF_PROFILE="${CHAFF_PROFILE:-$(perf_get_chaff)}"
    case "$CHAFF_PROFILE" in
        low|mid|off) ;;
        *) echo -e "${YELLOW}[!] Unknown chaff profile '${CHAFF_PROFILE}', defaulting to low.${NC}"; CHAFF_PROFILE="low" ;;
    esac
    LOCAL_PUB=${LOCAL_PUB:-$(ip route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}')}
    [[ -z "$LOCAL_PUB" ]] && LOCAL_PUB=$(curl -sSL --max-time 5 https://api.ipify.org 2>/dev/null)
    FRP_PORT=${FRP_PORT:-$(gen_random_port)}
    validate_setup_common "$LOCAL_PUB" "$REMOTE_PUB" "$FRP_PORT" "$LOCAL_GRE" || return 1
    is_valid_ip "$PEER_GRE" || { echo -e "${RED}[!] Invalid peer GRE IP: '$PEER_GRE'${NC}"; return 1; }
    if [[ -z "$TOKEN" ]]; then
        TOKEN=$(gen_token32)
        echo -e "${CYAN}[*] Generated token: ${TOKEN}${NC}"
    fi
    if tunnel_present && [[ "$FORCE" -ne 1 ]]; then
        echo -e "${RED}[!] Tunnel already exists — pass --force to overwrite.${NC}"
        return 1
    fi
    setup_iran_server_noninteractive "$LOCAL_PUB" "$REMOTE_PUB" "$FRP_PORT" "$TOKEN" "$LOCAL_GRE" "$PEER_GRE"
}

cli_setup_foreign() {
    local LOCAL_PUB="" REMOTE_PUB="" FRP_PORT="" LOCAL_GRE="" PEER_GRE="" TOKEN="" PORTS="" FORCE=0 BUNDLE=""
    local FOREIGN_GRE_DEF="$FOREIGN_GRE_IP" IRAN_GRE_DEF="$IRAN_GRE_IP"
    while [[ $# -gt 0 ]]; do
        case "$1" in
            --local-pub) LOCAL_PUB="$2"; shift 2 ;;
            --remote-pub) REMOTE_PUB="$2"; shift 2 ;;
            --frp-port) FRP_PORT="$2"; shift 2 ;;
            --local-gre) LOCAL_GRE="$2"; shift 2 ;;
            --peer-gre) PEER_GRE="$2"; shift 2 ;;
            --token) TOKEN="$2"; shift 2 ;;
            --ports) PORTS="$2"; shift 2 ;;
            --bundle) BUNDLE="$2"; shift 2 ;;
            --chaff) CHAFF_PROFILE="$2"; shift 2 ;;
            --force) FORCE=1; shift ;;
            -h|--help) usage_cli; return 0 ;;
            *) echo -e "${RED}[!] Unknown flag: $1${NC}"; usage_cli; return 1 ;;
        esac
    done
    CHAFF_PROFILE="${CHAFF_PROFILE:-$(perf_get_chaff)}"
    case "$CHAFF_PROFILE" in
        low|mid|off) ;;
        *) echo -e "${YELLOW}[!] Unknown chaff profile '${CHAFF_PROFILE}', defaulting to low.${NC}"; CHAFF_PROFILE="low" ;;
    esac
    if [[ -n "$BUNDLE" ]]; then
        bundle_parse "$BUNDLE" || { echo -e "${RED}[!] Bad --bundle (want hsh1_<IRAN_PUB>_<PORT>_<IRAN_GRE>_<FOREIGN_GRE>_<TOKEN>[_<PORTS>]).${NC}"; return 1; }
        [[ -z "$TOKEN" ]] && TOKEN=$B_TOKEN
        # setup-foreign runs on Foreign: bundle Iran pub is OUR remote,
        # bundle foreign GRE is OUR local, bundle Iran GRE is OUR peer.
        [[ -z "$REMOTE_PUB" ]] && REMOTE_PUB=$B_IRAN_PUB
        [[ -z "$FRP_PORT" ]] && FRP_PORT=$B_FRP_PORT
        [[ -z "$LOCAL_GRE" ]] && LOCAL_GRE=$B_FOREIGN_GRE
        [[ -z "$PEER_GRE" ]] && PEER_GRE=$B_IRAN_GRE
        [[ -z "$PORTS" ]] && PORTS=$B_PORTS
        echo -e "${CYAN}[*] Bundle applied: fields auto-filled (explicit flags kept).${NC}"
    fi
    # --bundle replaces --token as the required secret
    [[ -z "$TOKEN" && -n "$BUNDLE" ]] && TOKEN=$B_TOKEN
    LOCAL_PUB=${LOCAL_PUB:-$(ip route get 1.1.1.1 2>/dev/null | awk '/src/ {for (i=1; i<=NF; i++) if ($i=="src") {print $(i+1); exit}}')}
    [[ -z "$LOCAL_PUB" ]] && LOCAL_PUB=$(curl -sSL --max-time 5 https://api.ipify.org 2>/dev/null)
    FRP_PORT=${FRP_PORT:-$(gen_random_port)}
    LOCAL_GRE=${LOCAL_GRE:-$FOREIGN_GRE_DEF}
    PEER_GRE=${PEER_GRE:-$IRAN_GRE_DEF}
    validate_setup_common "$LOCAL_PUB" "$REMOTE_PUB" "$FRP_PORT" "$LOCAL_GRE" || return 1
    is_valid_ip "$PEER_GRE" || { echo -e "${RED}[!] Invalid peer GRE IP: '$PEER_GRE'${NC}"; return 1; }
    [[ -n "$TOKEN" ]] || { echo -e "${RED}[!] --token is required (copy it from the Iran side).${NC}"; return 1; }
    local CLEANED="" p
    for p in $(echo "$PORTS" | tr ',' ' '); do
        is_valid_port "$p" && CLEANED="$CLEANED $((10#$p))"
    done
    CLEANED=$(echo "$CLEANED" | xargs)
    [[ -n "$CLEANED" ]] || { echo -e "${RED}[!] --ports needs at least one valid port (e.g. \"443, 2083\").${NC}"; return 1; }
    if tunnel_present && [[ "$FORCE" -ne 1 ]]; then
        echo -e "${RED}[!] Tunnel already exists — pass --force to overwrite.${NC}"
        return 1
    fi
    setup_foreign_server_noninteractive "$LOCAL_PUB" "$REMOTE_PUB" "$FRP_PORT" "$TOKEN" "$LOCAL_GRE" "$PEER_GRE" "$CLEANED"
}

if [[ $# -gt 0 ]]; then
    check_root
    case "$1" in
        setup-iran) shift; cli_setup_iran "$@" ;;
        setup-foreign) shift; cli_setup_foreign "$@" ;;
        add-peer) shift; cli_add_peer "$@" ;;
        remove-peer) shift; cli_remove_peer "$@" ;;
        peer-list) peer_list ;;
        logs) show_logs ;;
        restart) restart_all ;;
        perf) shift; cli_perf "$@" ;;
        chaff) shift; cli_chaff "$@" ;;
        dpi-shield|dpi_shield|dpishield) shift; cli_dpi_shield "$@" ;;
        watchdog) shift; cli_watchdog "$@" ;;
        backup) shift; cli_backup "$@" ;;
        tgsend) shift; watchdog_send "$1" ;;
        update|update-all) update_all ;;
        peer-token)
            shift; ID=""
            while [[ $# -gt 0 ]]; do case "$1" in --id) ID="$2"; shift 2 ;; *) shift ;; esac; done
            peer_token "$ID" ;;
        status) check_status ;;
        panel-tls) shift; panel_tls_issue "$@" ;;
        optimize) tune_apply ;;
        restore) tune_restore ;;
        tune-status) tune_status ;;
        free-ram|optimize-ram) free_ram ;;
        remove-tunnel)
            if [[ "${2:-}" == "--force" ]]; then remove_tunnel_force; else remove_tunnel; fi ;;
        uninstall)
            if [[ "${2:-}" == "--force" ]]; then uninstall_all_force; else uninstall_all; fi ;;
        show-panel-url) show_panel_url ;;
        -h|--help|help) usage_cli ;;
        *) echo -e "${RED}[!] Unknown command: $1${NC}"; usage_cli; exit 1 ;;
    esac
    exit $?
fi
main_menu
