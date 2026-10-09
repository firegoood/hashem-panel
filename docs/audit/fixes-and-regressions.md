# Fork correction ledger

Baseline upstream: `52b41adab0ffb84ba02aeb18119cb6780a82d5ed` (`panel-r142`).
The candidate inventory, mechanisms and limits are in
[the audit](multipeer-gre-kcp-audit.md). Every row below is an **active** correction.
Latest integrated upstream: `80bbb012919c0cc27ef1e8c311310f03ae126c51` (`panel-r146`).
Previous delivery: `c71305de528280306aaa0c0b345e7f4c1eff47c4`; previous upstream: `dc9a6ee`.
The following comparison was actually performed during this delivery; active
rows remain only where upstream has not superseded their complete behavior.

| IDs | Cause and changed paths | Behavioral regression retained | Upstream replacement / removal condition |
|---|---|---|---|
| B01–B03, B07, B12, B14 | Divergent FRP generation and bundles. `managed_ports.go`, `managed_config.go`, `managed_api.go`, `managed_sync.go`, `setup.go`, `index.html`, `hashem.sh` | Normalize protocol/range/mapping; official native verify for peers 1–3; real two-peer KCP; CLI/WebUI mappings and transport change; secure/legacy JS parsing | Replace with upstream generator/bundle only when identical mapping, actual transport, GRE-only control, identity/revision and trust tests pass. Remove superseded paths, keep tests. |
| B04, B09, B16 | Process status mistaken for connectivity and remote apply. `managed_sync.go`, `tunnel.go`, UI | Real registration ACK, stale/pending/failed distinction, no forwarding claims without application response, pinned TLS and replay checks | Upstream must provide equivalent observed state and authenticated revision reconciliation, including disconnected/reordered updates. |
| B05, B08, B10, C06 | Broad port/service/interface cleanup. `managed_lifecycle.go`, `managed_legacy.go`, `managed_control.go`, `doctor.go`, Shell | Occupied listener survives; delete either peer preserves the other and unrelated GRE; rollback does not target the second peer | Prefer upstream ownership/preflight design if it establishes exact resources and passes both deletion orders and failed activation tests. Never restore global kill/reset behavior. |
| B06, B17–B18 | No recoverable apply; unsafely owned firewall and units. `managed_lifecycle.go`, `managed_firewall.go`, `managed_config.go`, `managed_control.go` | Inject failed create/update/remove/control; durable transaction recovery; exact rule cleanup; native service commands | Require staged native validation, persisted intent, unique commit marker, file/rule/service rollback and actual boot tests before replacing. |
| B11, B20 | String/array mismatch and truncating writers. `managed_registry.go`, `managed_lock_linux.go`, `managed_lock_other.go`, legacy Go/Shell writers | Mixed fixtures, partial error visibility without discard, 100 cross-process mutations; schema version guard | Keep legacy fixtures; replace only with upstream atomic schema/OS-lock implementation accepted by every writer. Future schema must not be silently rewritten. |
| B13 | Attractive fabricated fallback metrics and send-only UDP. `benchmark.go`, tests | All failed candidates produce no winner; UDP requires matching response; no fixed fake RTT/loss | Remove fork probe code when upstream passes no-response and all-failed tests and labels unavailable protocol-specific measurements correctly. |
| B15 | First-peer/global monitoring. `managed_watchdog.go`, Shell | Independent failure counters, no other-peer restart, capped exponential backoff | Upstream must monitor each peer independently and preserve disable state; revalidate legacy schedule/notification behavior. |
| B19, C01–C02 | Global carrier scope and insecure/fail-open WSS. `hashem.sh`, `wss_carrier.go` | Unrelated direct GRE survives; insecure default false; invalid TLS cert never opens WS | Keep explicit trust tests. A broader upstream carrier manager is preferred only after real direct/FOU/WSS per-peer isolation and migration tests pass. |
| C03–C05 | Public HTTP, weak password hash, peer CSRF bypass. `auth.go`, `password_hash.go`, `password_cli.go`, `secure_panel.go`, `peer.go`, `main.go` | Successful legacy hash migration; bounded PHC parsing; HTTPS redirect; HMAC pin/replay; existing auth/terminal security suite | Upstream replacement must preserve accounts, memory-hard hashing, session invalidation under concurrent password change, CSRF and fail-closed trust. Keep migrations until supported legacy accounts are migrated. |
| C07 | Floating installer/updater overwrites fork. `install.sh`, `install-fork.sh`, `update.go`, Shell, CI | Bash syntax/ShellCheck; checksum checks and restore source reviewed; real installation/upgrade gate pending | Re-enable online update only when it selects reviewed **fork** artifacts with mandatory digests/signatures, complete ownership checks and rollback. Never re-enable floating upstream scripts. |
| C08–C09 | Type truncation, path/XSS/secret exposure, unauthenticated backup integrity. `setup.go`, `tunnel.go`, `index.html`, `managed_backup.go` | Invalid JSON types/IDs rejected; log traversal rejected; numeric-only handler; API redaction; encrypted backup tamper/failure preserves registry | Prefer equivalent upstream validators and authenticated backup format after tests pass. New-host identity migration requires a tested automatic workflow. |
| C10 | Stress harness mislabel/backlog/half-close and Windows mode assertion. `frp_stress_test.go`, `rescue_test.go`, `setup_ports_test.go` | Linux relay concurrency and Windows bounded burst; close both directions; POSIX permissions tested on Linux; FRPS never owns client proxies | Keep truthful scope: these tests cannot become FRP capacity evidence. Retire portability correction when upstream tests work on both platforms. |

## Upstream comparison performed on 2026-10-09

| Upstream commit | Decision and reason | Retained regression / removal |
|---|---|---|
| `be79224` (passwords/sessions) | Adopt random per-login server-side sessions; remove the derivable cookie authentication path. Retain the fork's one Argon2id implementation: strict work bounds, error-returning entropy, atomic private storage, concurrent credential recheck. Accept upstream `argon2id$` and canonical `$argon2id$` formats and migrate successfully authenticated upstream accounts. | Adapt upstream password/session tests to the error-returning API; add actual upstream-format account migration. Delete duplicate upstream `password.go`; keep compatibility fixtures. C04 becomes ALREADY_FIXED relative to latest upstream. |
| `31e756e` (WSS) | Adopt no-browser Origin policy, constant-time token comparison and 10-second header timeout. Keep fork verified-TLS default/pinning and fail-closed startup; these address different failures. | Upstream Origin test plus fork untrusted TLS/no-plaintext tests retained. |
| `eb6ad68` (checksums) | Adopt checksum publication in release CI and fail-closed asset helper. Keep pinned Go/toolchain/dependencies and reviewed-checkout fork installation. Reject `GRE_PANEL_ALLOW_UNVERIFIED_UPDATE`; automatic upstream replacement remains disabled. Official manifest fetch is bounded to 1 MiB and must parse entries. | Retain missing/mismatch/correct digest tests; change opt-out test to require refusal. Re-enable an updater only under C07's full replacement condition. |
| `a2530b5` (peer leak test) | Existing fork transport already closes idle connections. Adopt the regression; no duplicate implementation or invented leak fix. | Forty real HTTP requests must leave zero open connections. |
| `12835f7` (obsolete units/RAM tuning) | Adopt legacy-unit cleanup but narrow ownership to exact `/usr/local/bin/hashem[.sh] --monitor/--webui` commands. Integrate with safe uninstall and explicit `cleanup-legacy`, preserving unrelated units. Adopt RAM-scaled values in the pre-existing legacy tuning workflow; managed setup does not invoke global tuning. Do not import broad upstream uninstall. | Fake systemd root tests: removed dead units, idempotence, retained valid/unowned units. Real upgrade/legacy tuning/reboot still unverified. Installer does not automatically run cleanup outside its rollback scope. |
| `dc9a6ee` (audit documents) | Preserve upstream historical audit/checklist attribution; their host observations are not this fork's test results. Fork audit and acceptance under `docs/` are authoritative for this delivery. | No claim that upstream host tests ran in this environment. |

## Follow-up review and integration: panel-r146

The pasted follow-up correctly identified missing interactive-menu coverage and
an actual hosted CI failure. Previous PASS results covered direct CLI/API paths,
not every interactive menu selection. The new review preserves that distinction.

| IDs / upstream | Cause, decision and changed paths | Retained verification and replacement condition |
|---|---|---|
| R01 CI checkout ownership | Original Actions run `37935268084` passed Go/race tests, then failed `go build` with `error obtaining VCS status: exit status 128`. The dependency scan was SKIPPED. `tests/integration/run.sh` trusts only `/work` in the disposable container so VCS stamping works across runner/root UID ownership. | Actual default build in the runner; keep mandatory vulnerability check. Never wildcard Git trust or mask test failures. |
| R02 menu KCP / R03 Fast Setup / R04 bundle / R05 help | `menu_add_peer` omitted transport; Fast Setup invoked legacy `cli_setup_foreign`; bundle display read the first legacy TOML; help prioritized hsh1. `hashem.sh` now shares `managed_cli`, defaults to KCP with explicit TCP choice, sends canonical JSON stdin, selects bundle by peer ID, and routes edit/remove/health/restart through owned management. | `tests/test_managed_menu.py` executes actual function bodies and menu dispatch. Real network integration creates TR through Add Peer and pairs through Fast Setup; retained exact TCP/UDP payload and removal isolation tests. Replace wrappers only when equivalent menu tests pass. |
| R06 newly verified foreign setup field mismatch | Existing guided foreign wrapper sent `local_pub`, while setup accepts `local_public`. It also exported the pairing secret to a Python child environment. The shared private-input wrapper now sends canonical `local_public` and bundle through stdin without argv/environment exposure. | Guided/Fast Setup request capture; actual native foreign setup; quoted-name/mapping serialization; failure cannot print a success banner. |
| `44e1d27` WSS front / Backhaul schema | Adopt TLS proxy command and v1/v2 Backhaul generators/tests. Narrow legacy WSS unit writes/removal to marker-owned units with valid suffix/port; refuse unrelated files and failed activation. Missing panel unit repair must use a reviewed fork checkout; never download upstream binaries through the menu. | Native Go TLS relay tests; fake-systemd WSS ownership/idempotence/invalid input tests and Backhaul version/schema fixtures. Full WSS/Backhaul deployment remains outside managed direct-GRE acceptance. |
| `133ead9` public dial/topology | Adopt independent legacy dial helpers/UI and base tunnel as a fleet spoke. Refuse legacy dial mutation when managed peers exist (Go API and Shell mutation/watchdog entry points). Public FRP fallback conflicts with managed GRE-only bind and is not silently enabled. | Legacy synthetic route fixture; `TestLegacyDialCannotMutateManagedPeers`; no topology ID collision with managed peers. Complete per-peer carrier/trust/rollback acceptance is required before managed fallback support. |
| R07 / `80bbb01` fleet health | Adopt shared dashboard/fleet rollup. Correct upstream socket-only HEALTHY inference: managed health requires a fresh authenticated registration, active owned resources and enabled state; blocked ICMP alone does not invalidate registration. Legacy socket-only evidence stays DEGRADED. | Adapt upstream linked/fleet regressions to authenticated evidence; `TestManagedFleetRequiresAuthenticationAndRegistration`; retain UNKNOWN application forwarding. |
| R08 WSS worker lifecycle | Run `37982018171` passed unit tests but caught a race between TLS startup and carrier API test teardown. `wss_carrier.go` now captures TLS paths before starting workers, joins cancellation before replacement/stop, rejects resource publication after cancellation, synchronizes running/timestamp state and interrupts reconnect waits. `carrier_test.go` and `wss_carrier_test.go` stop owned workers before restoring config; `managed_test.go` supplies explicit TLS paths. | `TestWSSCarrierImmediateStop` exercises server/client startup cancellation, concurrent status, joined worker completion and released TCP listeners; existing carrier/fail-closed TLS tests repeated under race five times. Replace with upstream only after equivalent start/stop/status race and resource-release tests pass. This is lifecycle evidence, not full WSS multi-peer acceptance. |

The review's other observations (no managed alternate carrier, UNKNOWN generic
application health, no real reboot/upgrade/migration or WAN evidence) are valid
scope limits, not newly reproduced implementation failures. An arbitrary
application protocol cannot be safely guessed from a port; no probe fabricates
an application-health success. These gates remain explicitly uncompleted.

## Procedure for the next Hashem Panel update

1. Read [AGENTS.md](../../AGENTS.md), check a clean status and preserve unrelated work.
2. `git fetch upstream`; record the old/new upstream SHAs. Create a dedicated
   `codex/upstream-<tag>` integration branch from the current fork branch.
3. Compare changed upstream behavior with **each active row**, including schema,
   dependencies, version support and configuration ownership. Inspect the diff
   before choosing merge, selected commits or a smaller replacement.
4. If upstream fixes the same defect, choose the implementation with better
   correctness, isolation, compatibility and maintenance cost. Prefer upstream
   when equivalent. Do not stack two fixes for the same behavior.
5. Record the superseding SHA and reason here. Remove redundant fork code only
   after the replacement passes the retained behavioral tests. Update the audit
   classification and deployment/migration documentation.
6. Run syntax, formatting, tests, race, vet, vulnerability checks, native FRP verify
   and disposable multi-peer acceptance. Test real systemd/VPS gates separately.
7. Push to the existing origin without force. Merge/release only after critical
   acceptance gates pass. A new upstream tag alone is insufficient evidence.

No local agent rule is a reason to preserve inferior fork code. The user's
current scope and verified behavior determine the integration choice.
