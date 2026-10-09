#!/usr/bin/env bash
# Install only from a locally reviewed checkout. Never execute a fetched script.
set -euo pipefail
umask 077
[[ $EUID -eq 0 ]] || { echo 'Run as root.' >&2; exit 1; }
[[ -d /run/systemd/system ]] || { echo 'A running systemd Linux host is required.' >&2; exit 1; }
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
[[ -f "$repo_dir/panel/go.mod" ]] || { echo 'Complete fork checkout required.' >&2; exit 1; }
stage_dir=$(mktemp -d)
trap 'rm -rf -- "$stage_dir"' EXIT
# Optional offline artifacts still pass the same pinned digest check. Copy into
# the private staging directory before checking/extracting to avoid cache races.
fetch_verified_artifact() {
    local url=$1 filename=$2 destination=$3 expected_hash=$4
    local artifact_dir=${HASHEM_INSTALL_ARTIFACT_DIR:-}
    if [[ -n "$artifact_dir" ]]; then
        [[ "$artifact_dir" = /* && -d "$artifact_dir" ]] || { echo 'Artifact directory must be an absolute existing directory.' >&2; return 1; }
        [[ -f "$artifact_dir/$filename" && ! -L "$artifact_dir/$filename" ]] || { echo "Required regular artifact missing: $filename" >&2; return 1; }
        cp -- "$artifact_dir/$filename" "$destination" || return 1
    else
        curl --proto '=https' --tlsv1.2 -fsSL --max-time 180 "$url" -o "$destination" || return 1
    fi
    printf '%s  %s\n' "$expected_hash" "$destination" | sha256sum -c -
}
case $(uname -m) in
    x86_64) arch=amd64; frp_hash=84f27e39f11169f7adcef8e8b70c9329de17747b1f14dad9fb95eef5682ea716; go_hash=63d339f0da5ab53635a56f2490a7984dfe12dfcff22ad749f63edaf590168445 ;;
    aarch64|arm64) arch=arm64; frp_hash=f33c293c275d8fc68c654b6fba8f10b2551d6463d09a9fc9cffb7227eae82266; go_hash=3450b45a3f9ee8568792736a5c5e70a1f2e9b36c35a8f74958c03e51d7d92bec ;;
    *) echo 'Supported architectures: amd64, arm64.' >&2; exit 1 ;;
esac
missing=0
for cmd in curl python3 ip iptables systemctl tar sha256sum; do command -v "$cmd" >/dev/null || missing=1; done
if [[ $missing -eq 1 ]]; then
    command -v apt-get >/dev/null || { echo 'Debian/Ubuntu dependencies required.' >&2; exit 1; }
    apt-get update -qq
    DEBIAN_FRONTEND=noninteractive apt-get install -y -qq ca-certificates curl python3 iproute2 iptables
fi
go_cmd=$(command -v go || true)
if [[ -z "$go_cmd" ]] || ! "$go_cmd" version | grep -Eq 'go1\.(2[6-9]|[3-9][0-9])\.'; then
    fetch_verified_artifact "https://go.dev/dl/go1.27.1.linux-${arch}.tar.gz" "go1.27.1.linux-${arch}.tar.gz" "$stage_dir/go.tgz" "$go_hash"
    tar -xzf "$stage_dir/go.tgz" -C "$stage_dir"
    go_cmd="$stage_dir/go/bin/go"
fi
fetch_verified_artifact "https://github.com/fatedier/frp/releases/download/v0.71.0/frp_0.71.0_linux_${arch}.tar.gz" "frp_0.71.0_linux_${arch}.tar.gz" "$stage_dir/frp.tgz" "$frp_hash"
# Expected archive and digest are pinned. Extract only the two regular binaries.
tar -xzf "$stage_dir/frp.tgz" -C "$stage_dir" "frp_0.71.0_linux_${arch}/frps" "frp_0.71.0_linux_${arch}/frpc"
fork_sha=$(git -C "$repo_dir" rev-parse --short=12 HEAD 2>/dev/null || echo source)
(cd "$repo_dir/panel" && CGO_ENABLED=0 "$go_cmd" build -trimpath -ldflags "-s -w -X main.panelVersion=fork-${fork_sha}" -o "$stage_dir/gre-panel" .)
"$stage_dir/gre-panel" peer-manage add-peer --help >/dev/null
if [[ -e /usr/local/bin/gre-panel && ! -f /etc/systemd/system/gre-panel.service ]]; then
    echo 'An existing unowned gre-panel binary was found; installation refused.' >&2; exit 1
fi
if [[ -f /etc/systemd/system/gre-panel.service ]] && ! grep -q 'ExecStart=/usr/local/bin/gre-panel' /etc/systemd/system/gre-panel.service; then
    echo 'An unowned panel service was found; installation refused.' >&2; exit 1
fi
cat > "$stage_dir/gre-panel.service" <<'UNIT'
[Unit]
Description=Hashem fork web panel
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=120
StartLimitBurst=5
[Service]
ExecStart=/usr/local/bin/gre-panel
Restart=on-failure
RestartSec=5
UMask=0077
NoNewPrivileges=yes
[Install]
WantedBy=multi-user.target
UNIT
# Validate the staged executable and unit before touching the installed files.
sed "s|ExecStart=/usr/local/bin/gre-panel|ExecStart=$stage_dir/gre-panel|" "$stage_dir/gre-panel.service" > "$stage_dir/verify-panel.service"
systemd-analyze verify "$stage_dir/verify-panel.service"
files=(/usr/local/bin/gre-panel /usr/local/bin/hashem.sh /usr/local/bin/hashem
       /usr/local/lib/hashem/frp-0.71.0/frps /usr/local/lib/hashem/frp-0.71.0/frpc
       /etc/systemd/system/gre-panel.service /etc/gre-panel/panel.json
       /etc/gre-panel/tls/server.crt /etc/gre-panel/tls/server.key)
mkdir "$stage_dir/snapshot"
for i in "${!files[@]}"; do
    if [[ -e "${files[$i]}" || -L "${files[$i]}" ]]; then
        [[ -f "${files[$i]}" || -L "${files[$i]}" ]] || { echo 'Unexpected installed file type.' >&2; exit 1; }
        cp -a -- "${files[$i]}" "$stage_dir/snapshot/$i"
    fi
done
was_active=0; was_enabled=0
systemctl is-active --quiet gre-panel.service && was_active=1
systemctl is-enabled --quiet gre-panel.service && was_enabled=1
rollback_install() {
    trap - ERR
    set +e
    systemctl stop gre-panel.service
    for i in "${!files[@]}"; do
        if [[ -e "$stage_dir/snapshot/$i" || -L "$stage_dir/snapshot/$i" ]]; then
            cp -a --remove-destination -- "$stage_dir/snapshot/$i" "${files[$i]}"
        else
            rm -f -- "${files[$i]}"
        fi
    done
    systemctl daemon-reload
    if [[ $was_enabled -eq 1 ]]; then systemctl enable gre-panel.service; else systemctl disable gre-panel.service; fi
    [[ $was_active -eq 0 ]] || systemctl restart gre-panel.service
    echo 'Installation failed; previous owned panel files restored. Inspect service diagnostics before retrying.' >&2
    exit 1
}
trap rollback_install ERR
install -d -m 0755 /usr/local/lib/hashem/frp-0.71.0
install -m 0755 "$stage_dir/frp_0.71.0_linux_${arch}/frps" "$stage_dir/frp_0.71.0_linux_${arch}/frpc" /usr/local/lib/hashem/frp-0.71.0/
install -m 0755 "$stage_dir/gre-panel" /usr/local/bin/gre-panel.new
mv -f /usr/local/bin/gre-panel.new /usr/local/bin/gre-panel
install -m 0755 "$repo_dir/hashem.sh" /usr/local/bin/hashem.sh
ln -sf /usr/local/bin/hashem.sh /usr/local/bin/hashem
if [[ ! -f /etc/gre-panel/panel.json ]]; then
    initial_password=$(python3 -c 'import secrets; print(secrets.token_urlsafe(24))')
    printf '%s' "$initial_password" | /usr/local/bin/gre-panel init-panel
    printf 'Initial administrator password (record securely): %s\n' "$initial_password"
    unset initial_password
fi
install -m 0644 "$stage_dir/gre-panel.service" /etc/systemd/system/gre-panel.service
systemctl daemon-reload
systemctl enable gre-panel.service
systemctl restart gre-panel.service
systemctl is-active --quiet gre-panel.service
trap - ERR
echo 'Reviewed fork installed. Existing peer configurations were preserved.'
python3 -c 'import json; c=json.load(open("/etc/gre-panel/panel.json")); print("Panel: https://SERVER:%d/%s/" % (c.get("tls_port",7443) or 7443,c["base_path"]))'
