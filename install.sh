#!/usr/bin/env bash
# Hashem one-line installer — downloads hashem.sh and runs it.
# Usage: bash <(curl -fsSL https://raw.githubusercontent.com/pdnczone/hashem-panel/main/install.sh)
set -euo pipefail
if [[ $EUID -ne 0 ]]; then echo "Please run as root (sudo)."; exit 1; fi
TMP="$(mktemp -d)"; trap 'rm -rf "$TMP"' EXIT
curl -fsSL https://raw.githubusercontent.com/pdnczone/hashem-panel/main/hashem.sh -o "$TMP/hashem.sh"
chmod +x "$TMP/hashem.sh"
exec bash "$TMP/hashem.sh"
