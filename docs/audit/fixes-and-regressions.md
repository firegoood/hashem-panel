# Fork correction ledger

Baseline upstream: `52b41adab0ffb84ba02aeb18119cb6780a82d5ed` (`panel-r142`).
The candidate inventory, mechanisms and limits are in
[the audit](multipeer-gre-kcp-audit.md). Every row below is an **active** correction.
Latest integrated upstream: `dc9a6ee765b6b2a91505cb4f67b8c7a35a58fa81`.
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
