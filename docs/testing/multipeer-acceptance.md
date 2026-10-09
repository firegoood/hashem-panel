# Acceptance — managed direct GRE + FRP 0.71.0

Date: 2026-10-09. Branch: `fix/multipeer-gre-kcp`.
Implementation commit: `66ce97f644dedb01f31efac9055693738e95c0cb`.
Integrated upstream: `af0c8ac2f916149011cb3abf5e0718d133445ed1` (`panel-r147`).
This original matrix reports the 2026-10-09 disposable environment below. No VPS
was accessed for that matrix. The later authorized real-VPS results are separate
in the dated section at the end; neither scope implies release readiness.

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


## Authorized real VPS validation, 2026-10-10

Starting fork: `cc35d5171f5da6148859c9504ad92ee4a8d39d34`; inspected upstream
unchanged at `af0c8ac2f916149011cb3abf5e0718d133445ed1`. Dedicated working branch:
`codex/real-vps-gre-kcp-validation`; delivery goes to the existing fork branch
`fix/multipeer-gre-kcp` by fast-forward. No newer upstream integration, main merge
or public release. Deployed code: `b86ffc7c09b884de7961659116ef983996ed2f6d`.
The current documentation commit is available through `git rev-parse HEAD`.

Actual environment: three user-authorized amd64 Ubuntu 24.04.5 LTS VPSs, kernel
6.8.0-146-generic, about 2 GiB RAM each; IR/NL have 2 vCPUs and TR has 1.
Official FRP 0.71.0 and genuine systemd are used. SSH host keys were verified
by the user before password authentication. Public evidence uses node labels;
raw inventory/configuration, live addresses, keys and pairing bundles are not
published. Four requested operational reports are local and excluded from Git.
See [sanitized machine-readable evidence](evidence/real-vps-2026-10-10.json).

**Outcome: overall deployment acceptance FAIL; release and complete three-node
operational readiness remain blocked.** NL GRE/KCP registered and forwarded
exact synthetic TCP/UDP responses on 8888 and 8889. TR configuration/unit/native
validation passes, but final managed GRE is unstable and FRPC does not register;
8880/2052 fail. Early temporary TR probes sometimes passed. Later paired
captures observe outbound GRE absent at the opposite host during that capture
window, including small packets. This supports off-host loss/blocking but does
not identify a specific ISP/provider device or prove a permanent all-time block.
No alternate carrier or direct-public control fallback was enabled.

NL bounded receiver throughput over actual FRP KCP: one TCP stream 5.046 Mb/s
at a 5 Mb/s cap; four TCP streams 20.152 Mb/s total at 5 Mb/s each, both 8 seconds
with zero iperf TCP retransmissions. UDP target 2 Mb/s, 1200-byte datagrams,
8 seconds: receiver 1.975 Mb/s, 1667 packets, zero loss and 4.993 ms jitter.
Direct public/GRE baseline around 8.04 Mb/s is reported separately and is not
KCP capacity. Attempted concurrent load gave NL 6.171 Mb/s; TR refused, so no
two-peer throughput measurement exists. TR direct TCP control connected but
carried zero test data; that is not a valid capacity measurement. Existing TR
iperf being busy was respected without restarting it. No 1000-user, saturation
or long-duration claim is made. Resource sampling overlapped part of the load;
global non-idle CPU includes VM steal and differs from owned process CPU.
Reverse NL-to-IR tests: 4.980 Mb/s one stream, 9.567 Mb/s four streams at the
same caps, with 0 and 33 reported TCP retransmissions respectively. These are
endpoint TCP counters, not KCP ARQ retransmission counts. An initial reverse UDP
attempt found the owned test server busy; an isolated retry through port 8889
received 1.999 Mb/s, 1666 datagrams, reported zero loss and 5.004 ms jitter.
The reverse 40-second resource window covers the TCP sequence and failed first
UDP attempt, not the UDP retry. NL whole-host non-idle CPU peaked at 100% for a
one-second sample while managed FRPC peaked at 3.98% of one core; the cause was
not isolated. After fixture shutdown, later no-load samples were 98-100% idle.
Concurrency was not escalated. These observations do not certify sustained load.

Actual NL FRPC crash recovery: 36.685 seconds to exact response with systemd
NRestarts increment. Brief owned GRE loss: 7.012 seconds down and 0.315 seconds
to exact response after link up. TR restart and disable/enable preserve NL
traffic/PID. Reciprocal functional continuity cannot be certified while TR is
disconnected. Native units verify/enable, but no machine was rebooted.

V01/V02 correct verified-artifact deployment and pinned-image CI limitations;
V03/V04 correct Fleet transport omission and first-load theme/i18n error.
Each application regression fails before correction and passes after it. Actual
headless Chrome on all three panels logs in with independent securely stored
passwords, shows authenticated NL and pending TR, displays KCP, logs out, and
reports zero JavaScript errors after correction. Generic application forwarding
health remains UNKNOWN. Hosted full CI
[37993816464](https://github.com/firegoood/hashem-panel/actions/runs/37993816464)
passes for the deployed code, including frontend, Shell, unit/vet/race/build,
26 native traffic/safety checks and mandatory verbose govulncheck. Scanner scope:
0 reachable vulnerabilities, 0 imported-package vulnerabilities, one required-
module advisory GO-2026-5932 in unused OpenPGP; no exclusion was added.

All task-owned temporary GRE, echo, benchmark and HTTPS transfer services/files,
artifact caches and the incomplete IR clone were removed. Reviewed source,
installed panels/managed peers and private recovery backups remain. Pre-existing
SSH/iperf service PIDs, SSH config hash, public routes/global sysctls and baseline
unrelated service activity are preserved. Host firewall has only peer-commented
added rules and unchanged ACCEPT policies. There are no real destination apps
on the requested foreign ports after synthetic fixtures were removed; the tested
NL tunnel therefore still needs application backends before operational use.

**Credential handling incident:** an initial raw read of the user attachment
included supplied SSH credentials in private tool output. T30 is FAIL, even
though none were committed and later workflows use private stdin/verified SSH.
Independent new panel passwords are stored only in current-user Windows
Credential Manager. The user should arrange SSH password rotation; SSH access
credentials were not changed automatically. This incident is not concealed by
the passing bounded journal/argv/unit secret checks.

| Case | Result | Evidence and scope |
|---|---|---|
| T01 | PASS | All three authorized nodes authenticated with user-verified host keys; later SSH remained available. |
| T02 | PASS | Read-only inventory on all three before mutation: OS/kernel/resources/interfaces/routes/listeners/firewall/SSH/workloads. |
| T03 | PASS | IR kernel GRE support and actual owned GRE creation. |
| T04 | PASS | NL kernel GRE support and actual owned GRE creation. |
| T05 | PASS | TR kernel GRE support and actual owned GRE creation. |
| T06 | PASS | IR/NL bidirectional GRE, exact TCP/UDP payloads and packet evidence. |
| T07 | FAIL | TR temporary early probes sometimes passed; final managed GRE remained unstable. Paired captures showed outbound small GRE missing at the opposite host during the capture window. |
| T08 | PASS | NL native FRP 0.71.0 KCP, fresh authenticated registration, public exact responses; 30 UDP control packets captured on owned GRE, 12 outbound/18 inbound, zero capture drops. |
| T09 | FAIL | TR KCP configuration validates and service runs, but no authenticated FRP registration; connection write timeouts. |
| T10 | PASS | Public NL 8888 TCP and UDP exact synthetic application replies, including independent external client. Test backend subsequently removed. |
| T11 | PASS | Public NL 8889 TCP and UDP exact synthetic application replies, including independent external client. Test backend subsequently removed. |
| T12 | FAIL | TR 8880 public TCP refused and UDP timed out; no registered proxy. |
| T13 | FAIL | TR 2052 public TCP refused and UDP timed out; no registered proxy. |
| T14 | FAIL | Attempted simultaneous load: NL succeeded, TR refused. Both active together was not achieved. |
| T15 | BLOCKED | Independent per-peer FRP/management secrets verified, but two simultaneous authenticated sessions require working TR. |
| T16 | BLOCKED | NL restart recovered; TR service PID/config preserved. Functional TR continuity could not be tested because TR was already disconnected. |
| T17 | PASS | TR owned restart preserved NL service PID and concurrent exact traffic. |
| T18 | PASS | NL native disable/enable closed/reopened its proxy and recovered exact response; other peer resources preserved. |
| T19 | PASS | TR native disable/enable preserved NL PID and exact TCP/UDP responses. This proves safe control/isolation, not successful TR reconnection. |
| T20 | PASS | Host policies preserved; only exact peer-commented rules added; GRE-only FRP control binds. Provider firewall/GRE forwarding was not inspectable. |
| T21 | PASS | All generated installed systemd units passed systemd-analyze verify and are enabled/active. This does not prove boot recovery. |
| T22 | BLOCKED | NL actual FRPC SIGKILL recovered payload in 36.685 s with NRestarts increment; NL restart also recovered. TR functional recovery remains blocked. |
| T23 | NOT_EXECUTED | No reboot authorization; existing workloads/other SSH sessions were preserved. |
| T24 | PASS | Authentic reviewed checkout, native installer and CLI stdin pairing; repeated installation on all three preserves accounts/configs/TLS. No normal TOML/unit/registry editing. |
| T25 | PASS | Actual headless Chrome via verified SSH tunnels: new login/logout, peer cards/Fleet match NL CONNECTED and TR pending; KCP visible and zero JavaScript errors after V03/V04 fixes. |
| T26 | PASS | Final SSH config hash and SSH/old iperf PIDs unchanged; public routes/global sysctls preserved; baseline unrelated services active. |
| T27 | BLOCKED | Bounded NL actual KCP and direct-path TCP/UDP measurements recorded. No usable TR throughput or concurrent two-peer result. |
| T28 | PASS | 25 one-second forward samples partially overlap load; 40 reverse samples cover bounded TCP sequence and its failed UDP attempt. CPU/RSS recorded on all three; retry UDP has no separate resource window. Global non-idle includes steal; not maximum/sustained capacity. |
| T29 | NOT_EXECUTED | Bounded authentication/CSRF/replay/permissions/known-secret/host-policy checks pass; whole legacy surface, full external secret/security scan and all-platform scan remain unverified. |
| T30 | FAIL | Initial raw attachment read exposed supplied SSH credentials in private tool output. No credentials were committed; new panel passwords use Windows Credential Manager and private stdin. Rotation of the exposed SSH passwords requires the user to arrange it. |
| T31 | NOT_EXECUTED | Private pre-install backup and encrypted managed backup dry-run verified; failed download preserved the prior IR install. A working full restore/host activation rollback was not exercised on these non-disposable machines. |
| T32 | PASS | V03/V04 application defects have failing-before/passing-after regressions and real browser verification. V01/V02 deployment/CI limitations corrected without disabling trust checks. V05 external network failure remains explicit. |
| T33 | PASS | Verified corrections and sanitized evidence committed on dedicated branch and fast-forward pushed to existing fork branch; no upstream/main/release write. |
| T34 | PASS | Hosted full CI green for deployed code b86ffc7c09b884de7961659116ef983996ed2f6d, run 37993816464; final documentation-only head checked separately. |

**21 PASS, 6 FAIL, 4 BLOCKED, 3 NOT_EXECUTED.** Runtime/provider TR repair and
retest, genuine application backends, coordinated reboot, real restore/rollback,
complete security/legacy coverage and sustained performance remain separate gates.
A green container/CI result cannot replace these real-host gates.
