# Managed GRE + FRP audit — 2026-10-09

Existing fork: https://github.com/firegoood/hashem-panel

Inspected upstream: https://github.com/pdnczone/hashem-panel

Starting fork and fetched upstream `main`: `52b41adab0ffb84ba02aeb18119cb6780a82d5ed`
(`panel-r142`, 2026-10-09). The fresh checkout had no custom diff or uncommitted
files. Work is isolated on `fix/multipeer-gre-kcp`; upstream is read-only.
The first delivery integrated `dc9a6ee765b6b2a91505cb4f67b8c7a35a58fa81`.
This follow-up starts at fork `c71305de528280306aaa0c0b345e7f4c1eff47c4` and
integrates upstream `80bbb012919c0cc27ef1e8c311310f03ae126c51` (`panel-r146`).
All three new upstream commits were reviewed with explicit resolutions; see the correction ledger for replacement decisions.
The current delivery SHA is obtained with `git rev-parse HEAD`; this report does
not contain a self-referential commit hash. AGPL-3.0 and upstream attribution remain.

## Method and scope

Reviewed the Shell installer/menu/lifecycle/firewall/backup paths and panel
setup, registry, status, carriers, peer auth, passwords, monitoring, benchmark,
update, terminal, systemd templates and CI. Candidate classification describes
the starting code, not a claim that every component has now been verified on VPS.
At the starting baseline all 30 candidate groups had a confirmed mechanism.
Against the final inspected upstream the classification is **CONFIRMED 29,
ALREADY_FIXED 1 (C04), NOT_APPLICABLE 0, NEEDS_RUNTIME_VERIFICATION 0**.
C04 was fixed by upstream `be79224`; the integrated fork retains bounded
Argon2id parsing/atomic migration and adopts random server-side sessions.
These counts classify source defects, not runtime acceptance: unexecuted
operational gates remain below. Other already-correct subcases are not additional
counted findings.
Runtime verification and remaining partial corrections are stated separately.

Use `git show 52b41ad:PATH` to inspect original functions. Named tests below are
in `panel/managed_test.go` unless another file is stated. Reproduce current tests
with `go test -run NAME ./...` from `panel`, or the disposable integration runner
in [acceptance](../testing/multipeer-acceptance.md). Source inspection plus actual
native FRP verification and traffic, rather than README assertions, support these findings.

| ID | Severity | Original affected path/function and exact failure | Evidence / reproduction | Required / implemented / regression verification |
|---|---|---|---|---|
| B01 | High | `hashem.sh:cli_add_peer`: additional FRPS config omitted KCP listener | Select KCP for peer 2; inspect `kcpBindPort` and UDP listener | Yes / managed generator / `TestManagedNativeFRPValidation` first, second, third; real two-peer KCP PASS |
| B02 | High | `cli_edit_peer`, `setup.go:editPeerDirect`, `rewriteTomlPorts`: client proxies written into server config | Edit FRPS ports; original adds `[[proxies]]` | Yes / shared manager + server allowPorts rewrite / `TestRewriteFRPSPortsNeverCreatesClientProxies`; CLI/WebUI remote mappings PASS |
| B03 | High | Shell edit updates `frp_transport` JSON without changing foreign config | Change KCP to TCP and inspect FRPC | Yes / desired revision, native verify, signed pull, ACK / actual transport-change integration PASS |
| B04 | High | Shell setup and WebUI equate active service with tunnel success | Start FRPS without FRPC and inspect success message | Yes / PENDING until registered, application forwarding UNKNOWN / failed-create tests and status API PASS |
| B05 | Critical | `hashem.sh` port cleanup and `doctor.go` global `fuser`/`pkill` | Occupy a required port with an unrelated listener | Yes / never kill to free ports; doctor stops only its own child / `TestManagedCollisionValidation`, source search PASS |
| B06 | High | Original create/edit/remove writes files and activates before registry, with no complete rollback | Fail firewall/service activation after writes | Yes / durable per-peer journals and rollback, including control/remove/restore / rollback injection tests PASS; real installer power-loss NOT_EXECUTED |
| B07 | High | Original FRPS control `bindAddr=0.0.0.0` | Inspect public control sockets | Yes / GRE bind plus public proxyBindAddr / native configs + socket inspection + public forwarding PASS |
| B08 | High | Legacy collision checks incomplete and sometimes free occupied ports forcibly | Duplicate subnet/token/mapping or unrelated UDP listener | Yes / validate registry and actual listeners/routes/interfaces/units before mutation / collision tests PASS |
| B09 | High | No per-peer desired revision or remote activation ACK | Edit central ports with foreign node disconnected | Yes / pinned HTTPS + signed GET/ACK, reject stale revisions, keep PENDING / sync tests + remote port edits PASS; changing live public addresses NOT_EXECUTED |
| B10 | Critical | First-peer removal falls through global `remove_tunnel_force` | Remove peer 1 with peer 2 active | Yes / exact owned peer teardown and legacy ownership guard / delete both orders + unrelated GRE preservation PASS |
| B11 | High | Shell Backhaul `raw_ports` string fails Go `[]string` decode, hiding entire list | Mixed string/array registry | Yes / compatible record reader reports errors without erasing valid records / compatibility/corrupt-preservation tests PASS |
| B12 | High | Shell parsing silently filters ranges and mappings; CLI/WebUI differ | Supply `8080=80`, range or protocol selection | Yes / one normalized parser; explicit invalid/duplicate errors / normalization tests + real mapped ports PASS |
| B13 | High | `benchmark.go` treats UDP send as success and fabricates RTT/loss fallbacks | Target a UDP endpoint that never replies | Yes / matching-response UDP probe, unavailable/failed metrics, no fabricated winner / UDP and benchmark tests PASS |
| B14 | Medium | Shell/UI bundle paths lose/ignore transport and omit peer identity/revision | Parse a transport-bearing legacy bundle in browser | Yes / hsh2 identity/revision/mappings/trust and UI transport parsing / secure/legacy bundle tests and JS test PASS; legacy hsh1 provisioning is deliberately refused for new managed foreign setup |
| B15 | High | Shell watchdog checks one fixed GRE/FRP pair | Fail second peer while first stays healthy | Yes / per-peer counters, five bounded restarts, exponential backoff, shared Shell monitoring / independent watchdog test PASS; legacy notification/scheduling parity remains partial |
| B16 | High | UI `gre_up || frp_up` shows tunnel active | GRE only or expired last ACK | Yes / process, GRE, auth, registration, desired/observed transport and UNKNOWN forwarding separated / status integration PASS |
| B17 | High | Firewall openings have no consistent per-peer ownership or GRE restriction | Compare rules after add/edit/remove | Yes / exact commented iptables rules, no global flush, boot restore descriptor / real namespace rule cleanup PASS; UFW/nftables policy/reboot compatibility NOT_EXECUTED |
| B18 | High | StartLimit directives placed in Service; weak GRE/FRP ordering | Inspect generated units; restart without GRE | Yes / Unit limits, Requires/After own GRE, oneshot GRE readiness / template/native supervisor checks PASS; actual systemd/reboot NOT_EXECUTED |
| B19 | High | `carrier_apply` defaults enumerate all GRE interfaces | Switch carrier with unrelated GRE present | Yes / default scope reduced; managed lifecycle permits direct GRE only and rejects alternate migration / unrelated direct-GRE test PASS; FOU/WSS multi-peer operation NOT_EXECUTED and unsupported in new lifecycle |
| B20 | High | Go and Shell registry writers truncate and race without shared lock | Concurrent read-modify-write across processes | Yes / flock + fsync/rename, atomic Go legacy writes, locked atomic Backhaul add / cross-process 100-mutation test PASS; broad legacy Backhaul lifecycle runtime NOT_EXECUTED |
| C01 | High | `defaultWSSConfig` defaults InsecureTLS true | Read default; connect with untrusted certificate | Yes / default verification plus explicit fingerprint option / WSS default test PASS; existing explicit insecure opt-in remains a legacy risk |
| C02 | Critical | WSS `runServer` TLS setup failure falls through to plain WS | Supply malformed cert | Yes / return error, no plaintext listener / `TestManagedWSSFailsClosed` PASS |
| C03 | Critical | Shared peer secret used on public HTTP; cookie auth bypasses CSRF; old protocol has no replay protection | Inspect sendToPeer fallback and authenticated POST middleware | Yes / new per-peer HTTPS pin/HMAC/body/time/nonce, no redirects, cookie POST CSRF, legacy public HTTP blocked / TLS/replay tests PASS; old TOFU/token protocol remains a limited legacy risk |
| C04 | High | Baseline admin password is unsalted fast SHA-256; ALREADY_FIXED in final upstream `be79224` | Compare baseline and upstream password implementations | Integrated / one bounded Argon2id implementation, accepts both PHC formats, atomic migration, adopts upstream random session design / retained upstream and concurrent-migration regressions PASS |
| C05 | High | Public login/admin HTTP listener accepts credentials | POST login over public HTTP | Yes / HTTPS redirect and automatic TLS; terminal remains opt-in / redirect and existing terminal/security tests PASS; browser/domain onboarding NOT_EXECUTED |
| C06 | Critical | Global GRE reset, interface enumeration, teardown and doctor tuning can affect unrelated resources | Inspect reset/uninstall/doctor paths | Yes / managed owned teardown, no automatic doctor sysctl/BBR/MSS changes / unrelated-resource integration PASS; old opt-in tuning and Backhaul paths remain outside verified scope |
| C07 | Critical | Installer/update executes floating upstream/mirror scripts; checksums can be optional | Inspect install/update source and URL construction | Yes / reviewed local checkout installer, pinned SHA256 FRP/Go, fork overwrite blocked, staged install rollback / Bash/ShellCheck PASS; real install/update rollback and legacy Backhaul supply chain NOT_EXECUTED |
| C08 | High | Floating IDs/ports truncate; invalid types ignored; log prefixes permit path traversal; peer names enter onclick string | Fractional JSON, `frps-../../...`, quoted name | Yes / bounded bodies/types/IDs, exact unit allowlist, numeric onclick only / input/log/JS tests PASS; entire legacy feature surface is not a security certification |
| C09 | High | Registry/status expose tokens; default startup logs generated password; secret flags reach child args | Inspect `/api/peers` and process invocation | Yes / status redaction, no-store explicit bundle endpoint, private files, stdin automation, no startup plaintext password journal, authenticated encrypted managed backup / tests PASS; full legacy log/backup audit remains partial |
| C10 | Medium | TCP relay stress tests labeled FRP capacity; one-way half-close leaks goroutines/FDs; Windows assumes POSIX mode bits | Run original Windows stress/rescue tests; inspect relay close direction | Yes / accurate names, half-close both directions, platform-specific permission assertion; additional unknown-ID action/default service bug fixed / Linux+Windows suites PASS; no native 1,000-user capacity claim |

## Previously correct subcases

- Final upstream `eb6ad68` fixes the optional updater checksum subcase and publishes checksums; C07 remains confirmed because reviewed fork ownership/installation is still needed. Its unverified opt-out is not adopted.
- Final upstream `31e756e` adds WSS Origin rejection, constant-time token comparison and header timeout; all are adopted alongside C01/C02 corrections.
- `sendToPeer` already closes its transport; upstream `a2530b5` adds useful connection-leak regression coverage, retained without another implementation.
- Legacy first-server FRPS already writes `kcpBindPort`; B01 concerns additional peers.
- Go `MakeBundle`/`ParseBundle` already carries `tr-kcp`; B14 concerns incomplete Shell/UI paths and missing identity/revision/trust.
- Root terminal is already disabled by default, with session/Origin/CSRF safeguards;
  C05 concerns public HTTP exposure, and C03 the peer cookie bypass.
- Upstream `52b41ad` already moved the rescue test to an unprivileged port. Its
  Windows POSIX-mode assertion was a separate remaining portability defect.

## Follow-up findings R01-R08

All eight source/CI defects below are **CONFIRMED**. Combined with the original
latest-upstream classification this is **CONFIRMED 37, ALREADY_FIXED 1,
NOT_APPLICABLE 0, NEEDS_RUNTIME_VERIFICATION 0** for counted defects. Operational
acceptance remains separate; missing runtime gates are not counted as fixed.

| ID | Defect / evidence | Correction and regression |
|---|---|---|
| R01 | Hosted CI unit/race PASS, binary build fails Git VCS status from runner/root ownership; scan skipped | Trust only mounted reviewed `/work`; actual CI build must pass; preserve scan gate |
| R02 | `menu_add_peer` omits transport and defaults new native peer to TCP | Explicit menu TCP/KCP selection, KCP default; generated TR KCP and traffic tested |
| R03 | Fast Setup dispatches to legacy hsh1 parser | Secure hsh2 stdin to native setup; actual menu pairing tested |
| R04 | Bundle menu reads one legacy `/etc/frp/frps.toml` | Select ID and call owned `peer-token`; actual dispatch fixture |
| R05 | CLI help and editing imply obsolete bundle/global carrier behavior | Managed-first help; normalized raw mappings and direct-GRE TCP/KCP edit menu |
| R06 | Guided foreign setup uses wrong `local_pub` field and exports bundle to child environment | Canonical `local_public`, private stdin; capture fixture and real setup |
| R07 | New upstream fleet labels established TCP socket as healthy without authenticated registration | Shared rollup requires managed authenticated registration; stale/pending/disabled/process-only never healthy; legacy socket-only DEGRADED |
| R08 | Hosted run `37982018171` reproduced a WSS worker reading the TLS config path after the carrier API test restored `configDir`; stop did not join startup and could leave late listeners | Snapshot TLS paths before spawning, cancellation guards on resource publication, joined stop, atomic running/status timestamp synchronization and cancelable reconnect waits; API tests stop workers before config restore; repeated immediate server/client stop and race regression |

The retained correction ledger records the exact new upstream decisions and
removal conditions. No full managed FOU/WSS/QUIC failover or generic application
probe is claimed. Original interactive menu gaps were not covered by the first
delivery's direct-command/API acceptance; the new fixtures close that gap.

## Remaining gates

This is a tested managed direct-GRE TCP/KCP implementation, **not a release**.
Actual systemd ordering/reboot, VPS firewall frameworks, installer upgrades and
power-loss behavior, full legacy migrations, alternate carrier parity, native
capacity/long-duration/loss/MTU performance, new-host TLS identity recovery and
dependency vulnerability results remain unverified or partial. `govulncheck`
could not fetch its official database (HTTP 403). No supplied production VPS was
accessed. No release/default-branch merge is allowed until those gates close.

Configuration authority has moved into localized `panel/managed_*.go` because
separate Shell and Go writers produced demonstrably incompatible ownership,
transport, registry and deletion semantics. Existing non-FRP features remain;
their unverified limitations are explicit rather than silently declared safe.

FRP syntax checked against [official server configuration](https://gofrp.org/en/docs/reference/server-configures/)
and [official client configuration](https://gofrp.org/en/docs/reference/client-configures/),
then verified by official FRP **0.71.0** binaries. KCP/ARQ is provided by FRP;
no separate ARQ service is installed.
