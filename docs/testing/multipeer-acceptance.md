# Acceptance — managed direct GRE + FRP 0.71.0

Date: 2026-10-09. Branch: `fix/multipeer-gre-kcp`.
Implementation commit: `66ce97f644dedb01f31efac9055693738e95c0cb`.
Integrated upstream: `af0c8ac2f916149011cb3abf5e0718d133445ed1` (`panel-r147`).
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

- Full Linux suite: 150 PASS, 0 FAIL, 0 SKIP (97.559 seconds after panel-r147 integration).
- Full Linux race suite: 150 PASS, 0 FAIL, 0 SKIP (101.771 seconds).
- Full Windows suite: 148 PASS, 0 FAIL, 2 SKIP (96.527 seconds). Both skipped
  native Linux FRP checks executed on Linux; POSIX file mode assertions are not
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
  exact payload checks. See [sanitized evidence](evidence/isolated-r147-2026-10-09.json).
- Static `systemd-analyze verify` for the two actual generated foreign units:
  PASS. This does not execute systemd boot/ordering/restart policy.
- Obsolete-unit and WSS-front ownership/idempotence regressions, Backhaul
  version/schema fixtures and legacy dial synthetic route fixture: PASS.
- Real interactive Add Peer default KCP and foreign Fast Setup with secure hsh2:
  PASS, including matching TCP/UDP traffic. Actual menu function/dispatch fixtures
  also cover TCP choice, bundle by ID, mapping edit, removal/restart, failed
  activation message and secret input/redaction. Prior evidence covered direct
  CLI/API only; the previously omitted menu limitations are corrected here.
- Dockerfile build and frontend inline JS/bundle transport checks: PASS.
- Upstream mux unset/on/off persistence and official FRP validation in both mux
  modes: PASS. Go/Shell global performance mutations refuse managed or corrupt
  registries before state changes; frontend disables these controls: PASS. Managed
  mux migration and throughput improvements are not claimed.
- Upstream SS RTT parser, ICMP/TCP selection and hub latency summary: PASS on
  synthetic fixtures. Authenticated managed health guards remain PASS; KCP does
  not get an invented TCP RTT when ICMP is unavailable.
- `govulncheck` v1.8.0: **PASS on the hosted Linux runner** at run `37984000607`.
  It reported 0 reachable vulnerabilities, 0 vulnerabilities in imported packages,
  and 1 advisory in required modules outside the imported call graph. Verbose
  run `37984925406` identifies GO-2026-5932 for unused `x/crypto/openpgp`; actual
  Linux dependencies include only `x/crypto/argon2` and `blake2b`. Local database fetch still returned
  HTTP 403; local scan is NOT_EXECUTED to completion. This is not a blanket
  certification that all dependencies or legacy features are vulnerability-free.

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

**26 PASS, 0 FAIL, 4 NOT_EXECUTED.** A22, A23, A27 and A29 plus unverified
alternate-carrier/legacy and full-security gates block release.

## Hosted CI follow-up

Original run [37935268084](https://github.com/firegoood/hashem-panel/actions/runs/37935268084)
failed after unit/race tests passed: default `go build` could not obtain Git VCS
status from a checkout owned by the runner but mounted in a root container.
`govulncheck` was **SKIPPED**, so it did not cause that failure. The runner now
trusts exactly `/work`, preserving VCS provenance without wildcard trust. The
subsequent successful binary build is confirmed by run `37984000607`.

Follow-up run [37982018171](https://github.com/firegoood/hashem-panel/actions/runs/37982018171)
passed unit tests but exposed a race in WSS startup versus carrier API test
teardown. The TLS path is now captured before spawning; stop cancels and joins
the worker, resource publication checks cancellation, status fields synchronize,
and the API tests stop owned workers before restoring their config paths. The
carrier/fail-closed/immediate-stop regressions passed five repetitions under race.
The hosted scan in that failed attempt was again SKIPPED.

Run [37984000607](https://github.com/firegoood/hashem-panel/actions/runs/37984000607)
at `66ce97f` passed the full workflow: frontend, image build, Shell/menu fixtures,
Go unit/vet/race, binary build, 26 real traffic checks, mandatory dependency scan
and sanitized artifact upload. A verbose scanner follow-up uses the identical
application/test source and reports the required-module advisory explicitly.

Final verbose run [37984925406](https://github.com/firegoood/hashem-panel/actions/runs/37984925406)
at `94f7d48` passed all workflow steps again. The only change from `66ce97f`
is scanner verbosity. [GO-2026-5932](https://pkg.go.dev/vuln/GO-2026-5932) applies
to the deprecated OpenPGP package and has no fixed version; it is not imported
by this Linux panel build. Scanner scope: five modules including the panel,
Go 1.27.2 standard library and Linux package call graph. No ignore rule,
database opt-out or weakened exit handling was added.

The local scan was retried against both the canonical compressed index and
canonical bulk archive; both returned HTTP 403. No database opt-out, empty local
database or fabricated scan result is used. Documentation-only pushes do not
rerun networking CI; all code, scripts, tests and workflow changes do.

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
