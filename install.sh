#!/usr/bin/env bash
# Hashem one-line installer — resilient multi-mirror download & auto-setup.
# Usage: bash <(curl -fsSL https://raw.githubusercontent.com/pdnczone/hashem-panel/main/install.sh)
set -euo pipefail

if [[ $EUID -ne 0 ]]; then
    echo "Error: Please run as root (sudo)." >&2
    exit 1
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

URLS=(
    "https://raw.githubusercontent.com/pdnczone/hashem-panel/main/hashem.sh"
    "https://mirror.ghproxy.com/https://raw.githubusercontent.com/pdnczone/hashem-panel/main/hashem.sh"
    "https://ghproxy.net/https://raw.githubusercontent.com/pdnczone/hashem-panel/main/hashem.sh"
    "https://fastly.jsdelivr.net/gh/pdnczone/hashem-panel@main/hashem.sh"
)

DOWNLOADED=0
for U in "${URLS[@]}"; do
    if curl -fsSL --connect-timeout 8 --max-time 40 "$U" -o "$TMP/hashem.sh" 2>/dev/null && [[ -s "$TMP/hashem.sh" ]]; then
        if bash -n "$TMP/hashem.sh" 2>/dev/null; then
            DOWNLOADED=1
            break
        fi
    fi
done

if [[ "$DOWNLOADED" -ne 1 ]]; then
    echo "Error: Failed to download hashem.sh from GitHub or fallback mirrors." >&2
    exit 1
fi

chmod +x "$TMP/hashem.sh"
mkdir -p /usr/local/bin
cp "$TMP/hashem.sh" /usr/local/bin/hashem.sh
cp "$TMP/hashem.sh" /usr/local/bin/hashem
chmod +x /usr/local/bin/hashem.sh /usr/local/bin/hashem
ln -sf /usr/local/bin/hashem.sh /usr/local/bin/gre.sh 2>/dev/null || true

exec bash /usr/local/bin/hashem "$@"
