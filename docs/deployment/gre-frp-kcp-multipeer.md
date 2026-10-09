# Automated two-peer deployment and maintenance

**Status: controlled testing only. Release blocked.** First close the real
systemd/reboot, firewall, upgrade/migration, secret-audit and dependency gates in
[acceptance](../testing/multipeer-acceptance.md). This document is an execution
recipe for a subsequently authorized test host; no production machine was changed.
Use the existing fork and reviewed commit, not the upstream floating installer.

## Architecture

| Peer | Iran GRE | Foreign GRE | Control | Iran public → foreign local | Transport |
|---|---|---|---|---|---|
| NL, ID 1 | 10.70.1.1/30 | 10.70.1.2/30 | 17001 | 8888→8888, 8889→8889 | FRP KCP over GRE |
| TR, ID 2 | 10.70.2.1/30 | 10.70.2.2/30 | 17002 | 8880→8880, 2052→2052 | FRP KCP over GRE |

Supported new lifecycle: Linux/systemd, IPv4, direct kernel GRE, FRP 0.71.0 TCP
or KCP. Unqualified ports generate TCP and UDP; use `tcp:8888`, `udp:2052`,
`tcp:8080=80`, `udp:8880-8889`, or mapped equal-length ranges for selection.
GRE hosts must be usable private addresses in the same /30. The manager rejects
collisions rather than terminating unrelated applications. FOU/WSS/QUIC and
legacy Backhaul workflows are not certified by these tests.

Each peer owns `hsh-gre-ID.service`, `hsh-frps-ID.service` on Iran or
`hsh-frpc-ID.service` abroad, `/etc/gre-panel/managed/ID/`, and firewall rules
commented `hashem:peer:ID`. FRPS control binds its GRE address; public proxy
listeners bind publicly. Both TCP/KCP control listeners remain on GRE during
transport transitions. FRP provides KCP ARQ itself. HTTPS management defaults
to 7443; initial foreign bootstrap/repair accepts only its signed, pinned peer.

## Install the reviewed fork on each test node

Debian/Ubuntu amd64/arm64, root, working systemd and provider support for IP
protocol 47 are required. Ensure application services already listen on the
foreign local ports. Configure any provider-level firewall through its normal
administration; the panel manages host rules only and never modifies SSH rules.

```sh
git clone https://github.com/firegoood/hashem-panel.git /opt/hashem-panel-fork
cd /opt/hashem-panel-fork
git checkout fix/multipeer-gre-kcp
git rev-parse HEAD                 # compare with the delivered reviewed SHA
bash install-fork.sh
```

The installer verifies pinned official FRP archives and a pinned Go bootstrap
when needed, builds the checked-out source, validates the staged panel unit,
backs up previous owned files, activates the panel and restores those files on
ordinary activation error. Dependency installation is not rolled back; sudden
host power loss during installer file swaps has not been tested. Peer configs
are preserved. It refuses an unowned existing panel binary/unit. Record the
initial password printed in the root installation terminal securely. The
service never logs a generated plaintext login password.

Open the printed HTTPS URL/base path. A self-signed management certificate is
created automatically. Compare its fingerprint through the trusted installation
channel before accepting it in a browser; optional public domain certificates
remain available. Root terminal stays disabled unless explicitly enabled.

## Create NL and TR on Iran

Replace only `IRAN_PUBLIC_IP`, `NL_PUBLIC_IP`, `TR_PUBLIC_IP` below. There is no
FRP TOML, registry, unit or firewall editing. JSON arrives through stdin so
credentials are not placed in process arguments. Generated output contains a
secret `bundle`; copy it privately to the intended foreign node.

```sh
hashem add-peer --request-file - <<'JSON'
{"id":1,"name":"NL","local_public":"IRAN_PUBLIC_IP","remote_public":"NL_PUBLIC_IP","local_gre":"10.70.1.1","peer_gre":"10.70.1.2","frp_port":17001,"frp_transport":"kcp","ports":"8888,8889"}
JSON
hashem add-peer --request-file - <<'JSON'
{"id":2,"name":"TR","local_public":"IRAN_PUBLIC_IP","remote_public":"TR_PUBLIC_IP","local_gre":"10.70.2.1","peer_gre":"10.70.2.2","frp_port":17002,"frp_transport":"kcp","ports":"8880,2052"}
JSON
```

The output is **PENDING**, meaning local resources activated. It does not prove
foreign authentication or application data delivery. Fetch a bundle again with
`hashem peer-token --id 1` (or 2); this is an intentional secret-display action.
Do not paste bundles into tickets, logs, Git or public chat.

## Pair each foreign node

On NL run the following, paste **NL's** `hsh2_...` bundle and finish the prompts:

```sh
hashem menu
```

Choose foreign FRP setup. It reads the bundle silently and the local public IP,
then submits JSON through stdin. Repeat on TR with **TR's** bundle. Automated
equivalent: `hashem setup-foreign --request-file -` with a private JSON stdin
object `{"bundle":"<hsh2 bundle>","local_public":"<this node public IPv4>"}`.
An old hsh1/token-only bundle cannot establish the new authenticated management
trust; recreate a secure bundle from the central managed peer. Existing legacy
services are retained until an explicitly validated migration.

WebUI equivalent: on Iran select GRE+FRP, TCP/KCP, public endpoints, GRE /30,
control port and forwarded mappings in Setup. Use Add another foreign server
for TR. On each foreign panel paste the correct hsh2 bundle. Peer cards expose
the same manager and desired state; edits go through the central panel. Backend
rejects unsupported carrier/engine migrations without changing running state.

## Verify and operate

`hashem peer-list` returns redacted desired state. In WebUI inspect GRE, process,
registration, desired/observed transport and last verification separately.
Foreign panels reconcile every 15 seconds; CLI `hashem reconcile --id ID` makes
an immediate attempt. A verified proxy registration produces CONNECTED; forwarding
remains UNKNOWN until an application-specific response has been verified. Test
TCP/UDP against each real application from an independent client. UDP send alone
and an open TCP socket do not establish application delivery.

```sh
hashem edit-peer-ports --request-file - <<'JSON'
{"id":1,"raw_ports":["tcp:8888","udp:8888","tcp:9999=8889"]}
JSON
hashem edit-peer --request-file - <<'JSON'
{"id":1,"frp_transport":"tcp"}
JSON
hashem disable-peer --id 1
hashem enable-peer --id 1
hashem restart-peer --id 1
hashem health-check
hashem remove-peer --id 1
```

Central disable stops/disables that central peer's units; it does not shut down
the foreign application or delete foreign resources. The foreign service may
continue reconnecting until separately disabled. Remove the corresponding
foreign peer with the same command on that foreign node when decommissioning.
Restart clears stale connection evidence. Remote changes stay pending until
the foreign revision ACK arrives. A rejected activation restores that peer's
previous files/rules/services; recovery retries pending journals under the lock
on the next mutation or `hashem recover`. Review an explicit rollback repair
error instead of ignoring it.

## Backup, restore and updates

```sh
hashem backup now
gre-panel peer-manage restore-backup --file /var/backups/hashem/ARCHIVE.enc --dry-run
hashem backup restore /var/backups/hashem/ARCHIVE.enc
```

Managed backups use AES-256-GCM authenticated encryption, contain peer configs
and server keys without arbitrary extraction paths, and exclude the encryption
key. Securely retain `/etc/gre-panel/managed-backup.key` separately. Ordinary
same-node restore validates identities, regenerates units/configs/firewall,
and journals each peer. Multiple-peer restore commits each successful peer
individually and reports the failing peer; it is not one all-node transaction.
Do not delete the central TLS identity: changing it invalidates existing pins.
Automatic new-host TLS identity recovery remains a release gate. Existing legacy
archive restore is not certified by the new authenticated archive checks.

Future updates follow [AGENTS.md](../../AGENTS.md) and
[the correction ledger](../audit/fixes-and-regressions.md): compare every fork
fix with the new upstream implementation, keep the better behavior, retain tests,
and remove redundant code after verification. Install an approved fork checkout
with `install-fork.sh`. Automatic upstream replacement is blocked deliberately
to prevent loss of fork fixes. Do not use the original upstream install URL.

`remove-tunnel --force` removes explicitly owned FRP peers; it refuses unknown
engines instead of global teardown. Safe uninstall retains shared binaries,
configuration and recovery material. Review ownership before any later removal
of that retained data; other applications must remain operational.

Legacy dead `hashem-monitor`/`hashem-webui` units can be inspected and removed
with `hashem cleanup-legacy`; only exact owned obsolete commands qualify.
Reviewed installation does not automatically remove these legacy units, because
its rollback currently covers panel files rather than legacy service state.
Safe uninstall performs the narrow cleanup. RAM-scaled legacy optimization is
retained but does not run during managed peer provisioning and is not certified
as harmless to other applications; review it separately before opting in.

## Interactive menu and panel-r146 additions

Run `hashem menu`, then Tunnel Management. Add Peer chooses the next free owned
ID, suggests its independent GRE /30 and control port, asks for TCP/KCP, and
uses KCP when Enter accepts the default. It returns a private hsh2 bundle and
PENDING local activation. Foreign Fast Setup and Guided Setup both accept that
bundle silently and ask for **that foreign node's** public address; the correct
`local_public` field goes to native setup through stdin. List/Show Bundle asks
for the peer ID and does not read a first-peer legacy TOML. Edit supports full
protocol/range/mapped ports and TCP/KCP; health/restart/removal target owned peers.

Upstream panel-r146 adds a legacy WSS TLS front, Backhaul schema selection,
public-dial routing and hub/spoke display. These are retained with ownership and
health guards. Managed direct-GRE peers do not automatically dial public FRP
control ports: their control listeners intentionally bind to their GRE address.
The legacy Dial panel is unavailable when managed peers exist. Managed WSS/FOU/
QUIC support still requires per-peer trust, firewall, migration and rollback
acceptance. An active TCP socket alone is not authenticated health.

Legacy WSS stop/replacement now cancels and joins its startup/reconnect worker
before returning, preventing a canceled startup from leaving a late listener.
This local lifecycle correction does not certify the alternate-carrier gates.

Web Panel Repair starts an existing owned binary/unit. A missing unit or binary
requires `install-fork.sh` from the reviewed checkout; the menu never fetches a
floating upstream replacement. Installer ownership refusal is an explicit
migration gate, not a reason to overwrite a retained unowned binary.
