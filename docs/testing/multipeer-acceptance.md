# Acceptance — managed direct GRE + FRP 0.71.0

Date: 2026-10-09. Branch: `fix/multipeer-gre-kcp`.
Implementation commit: `c48f7bc96d43e6c67de64d044b8ee4616080b262`.
Integrated upstream: `dc9a6ee765b6b2a91505cb4f67b8c7a35a58fa81`.
This matrix reports the disposable environment below. **No production VPS was
accessed and no release readiness is implied by PASS.**

## Environment and reproducible commands

Windows host, Docker Desktop/WSL2 Linux kernel `6.6.114.1-microsoft-standard-WSL2`,
Debian 12 amd64, Go 1.27.2 in the Linux image, Go 1.27.1 on Windows, official
FRP 0.71.0, ShellCheck 0.9.0. Base image digest:
`sha256:5cf287a799e6b94384bad13d16b14904c531f51ba65792237e122ce42b392f61`.

```sh
docker build -t hashem-qa -f tests/integration/Dockerfile .
docker run --rm --privileged -v "$PWD:/work" hashem-qa bash tests/integration/run.sh
node tests/frontend-bundle.js
```

Run only the disposable container. The Python integration runner refuses a
non-container or non-root environment. It uses three network namespaces with
reserved `192.0.2.0/24` addresses, actual kernel GRE interfaces and official FRP
processes. The `tests/integration/systemctl` supervisor executes generated
commands and their dependencies; **it is not systemd and cannot verify reboot,
systemd restart policy or enablement persistence**. TCP and UDP backends return
the destination peer label plus the exact random payload. UDP send alone fails.
Shell CLI wrappers and authenticated HTTPS WebUI APIs operate the same manager.

## Executed checks

- Full Linux suite: 133 PASS, 0 FAIL, 0 SKIP (96.542 seconds after upstream integration).
- Full Linux race suite: 133 PASS, 0 FAIL, 0 SKIP (101.121 seconds).
- Full Windows suite: 132 PASS, 0 FAIL, 1 SKIP (88.797 seconds). The skipped
  native Linux FRP check executed on Linux; POSIX file mode assertions are not
  applied to Windows. The TCP relay burst on Windows is bounded to 64 clients.
- The final full race run includes concurrent credential migration plus upstream
  random-session, upstream Argon2id compatibility, checksum, Origin and peer
  connection-leak regressions. No derivable legacy cookie authenticates.
- `go vet ./...`, `gofmt -l ./*.go`, all root Shell `bash -n`, and
  `git diff --check`: PASS. Install scripts and integration runner ShellCheck:
  PASS. Whole legacy Shell inventory has existing warnings/style/info, with no
  ShellCheck error-level findings; it is not claimed warning-free.
- Native `frps verify`/`frpc verify`: TCP and KCP configurations for peers 1, 2,
  and 3 passed. Per-peer TLS configs, ranges/mappings and proxy ownership tested.
- Real multi-peer integration: four TCP and four matching-response UDP port
  tests; simultaneous KCP on independent GRE listeners; disable/enable both
  orders; delete both orders; CLI/WebUI port updates and actual transport switch;
  invalid update preservation; API secret redaction; application restart;
  idempotent repeat setup; owned interface/unit/firewall cleanup; unrelated GRE
  preservation; encrypted backup validation; 128 actual KCP TCP sessions with
  exact payload checks. See [sanitized evidence](evidence/isolated-2026-10-09.json).
- Static `systemd-analyze verify` for the two actual generated foreign units:
  PASS. This does not execute systemd boot/ordering/restart policy.
- Obsolete-unit ownership/idempotence regression using fake systemctl: PASS.
- Dockerfile build and frontend inline JS/bundle transport checks: PASS.
- `govulncheck` v1.8.0: **NOT_EXECUTED to completion**. Official database fetch
  `https://vuln.go.dev/index/modules.json.gz` returned HTTP 403. CI retries this
  as a blocking check. No claim that dependencies are vulnerability-free.

Native stress verifies 128 completed sessions with 32 concurrent workers, not
1,000 simultaneous FRP users. The separate 1,000-client Go relay test is a harness
test and says nothing about FRP capacity. Throughput, CPU/RSS/FD under sustained
native load, injected packet loss, real WAN RTT, MTU fragmentation and long-run
stability are **NOT_EXECUTED**. No attractive estimates substitute for them.

## A01–A30

| Case | Result | Evidence and scope |
|---|---|---|
| A01 Fresh setup without manual edits | PASS | Generated peer resources and pairing in pre-provisioned disposable Linux; real host installer remains unverified |
| A02 Both peers simultaneously connect | PASS | NL/TR labeled payload responses through separate FRP instances |
| A03 Actual KCP on both configurations | PASS | Native configs, registration, UDP control sockets and real traffic |
| A04 Non-overlapping GRE addresses | PASS | 10.70.1.0/30 and 10.70.2.0/30 plus collision rejection |
| A05 Both KCP listeners bind | PASS | `ss -uln` verifies GRE bind, no wildcard control bind |
| A06 TCP 8888 | PASS | Iran public socket → NL exact payload |
| A07 TCP 8889 | PASS | Iran public socket → NL exact payload |
| A08 TCP 8880 | PASS | Iran public socket → TR exact payload |
| A09 TCP 2052 | PASS | Iran public socket → TR exact payload |
| A10 Genuine UDP forwarding | PASS | Four ports require exact peer-labeled response |
| A11 Disable NL preserves TR | PASS | TR traffic continues; NL enable reconnects within bounded timeout |
| A12 Disable TR preserves NL | PASS | NL traffic continues; TR enable reconnects within bounded timeout |
| A13 Delete NL preserves TR | PASS | Real TR traffic continues |
| A14 Delete TR preserves NL | PASS | Recreated NL real traffic continues |
| A15 Edit NL preserves TR | PASS | WebUI mapping 9999→8889; TR config hash and traffic preserved |
| A16 Edit TR preserves NL | PASS | CLI mapping 9998→2052; NL config hash and traffic preserved |
| A17 Transport update operational | PASS | KCP→TCP changes native FRPC config, remote revision applied, traffic works |
| A18 Invalid config preserves connections | PASS | Duplicate public port rejected; both traffic/configs preserved; injected activation rollback |
| A19 CLI/WebUI desired-state equivalence | PASS | Same native core, HTTPS setup/update APIs plus actual Shell CLI edits |
| A20 No false fully connected deployment | PASS | PENDING/registration/UNKNOWN forwarding; invalid create fails without commit |
| A21 Application restart persistence | PASS | Restart panel process; stored peers and both traffic remain; login refreshes CSRF |
| A22 Supported restart/reboot recovery | NOT_EXECUTED | Supervisor stop/start reconnect passed; actual systemd boot/reboot policy untested |
| A23 Application upgrade preserves peers | NOT_EXECUTED | Staged reviewed installer/rollback implemented; real systemd upgrade not run |
| A24 No owned orphan resources | PASS | First peer directory, generated units, GRE link and commented firewall rules removed |
| A25 Unrelated services/interfaces untouched | PASS | Unrelated GRE unchanged; occupied listener preserved; exact owned child cleanup; scope direct-GRE lifecycle |
| A26 Concurrent requests preserve state | PASS | OS-lock test: Go goroutines plus a separate process, 100 mutations; valid final revision |
| A27 Existing legacy install compatibility | NOT_EXECUTED | Registry/bundle compatibility fixtures and ownership refusals pass; real old install migration untested |
| A28 Normal management needs no file editing | PASS | Managed setup, pair, mappings, transport, disable/enable/restart/remove/recreate/backup/restore are generated; alternate carriers excluded |
| A29 No secrets in logs/backups/Git | NOT_EXECUTED | Managed API/private-file/encrypted-backup tests pass; full legacy log/backup coverage and external secret scanner unavailable |
| A30 Repeated setup safe/idempotent | PASS | Same peer returns same revision/config hash without duplicate resources |

**26 PASS, 0 FAIL, 4 NOT_EXECUTED.** A22, A23, A27 and A29 plus the uncompleted
vulnerability check and unverified alternate-carrier/legacy gates block release.

## Failure and recovery coverage

The tests inject errors at native verification, firewall insertion, FRP restart,
daemon reload/removal and control operations. Existing files/registry and the
unrelated peer remain unchanged. Tampered backup authentication fails before
activation. Native test supervisor proves command execution, not actual systemd
rollback. Journals use a distinct operation marker so a same-revision foreign
restore cannot be mistaken for a committed interrupted operation.

CI in `.github/workflows/verify-fork.yml` runs on the dedicated branch/PR and
publishes only sanitized acceptance evidence. It does not build a public release
or merge to main. A successful local test is not a claim that hosted CI finished.
