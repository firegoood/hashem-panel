# Fork maintenance rules

This repository is the existing `firegoood/hashem-panel` fork of
`pdnczone/hashem-panel`. Preserve upstream attribution and AGPL-3.0 licensing.

## Scope and safety

- Work on a dedicated branch. Never force-push or write to upstream.
- Inspect status, remotes and the fork/upstream diff before editing. Preserve
  unrelated changes and do not reset or overwrite another task's work.
- Test networking and destructive lifecycle operations in disposable Linux
  environments. Previously supplied VPS access is not authorization to deploy
  or modify those machines for a new task.
- Manage only explicitly owned peer resources. A port conflict must fail before
  mutation; never kill an unrelated listener, flush firewall rules, or perform
  global teardown for one peer.
- Never commit credentials, pairing bundles, private keys, live server addresses,
  or sensitive test artifacts. Redact diagnostic evidence.

## Recording changes

- Record the starting fork and inspected upstream SHAs in the audit report.
- Give each verified defect an ID in `docs/audit/multipeer-gre-kcp-audit.md`.
  Distinguish confirmed defects, upstream fixes, inapplicable candidates and
  checks requiring runtime verification.
- For each correction, maintain `docs/audit/fixes-and-regressions.md` with its
  cause, affected paths, behavioral regression test, verification scope, and
  upstream comparison/removal condition. Do not keep a workaround without a
  documented reason.
- Keep `docs/testing/multipeer-acceptance.md` honest: PASS, FAIL or NOT_EXECUTED,
  with evidence and environment. Unit tests are not production VPS tests.
- Update deployment documentation whenever CLI, WebUI, generated configuration,
  migration, supported FRP version, or backup behavior changes.

## Applying future upstream updates

1. Fetch upstream without changing the current checkout. Record the old and new
   SHAs and inspect release notes, dependencies, schema and lifecycle changes.
2. Compare each active fork correction with upstream behavior and its regression
   test. Do not blindly merge, rebase or reapply patches.
3. If upstream fixes the same defect, choose the implementation that best
   preserves correctness, isolation, compatibility and maintainability. Prefer
   the upstream implementation when it provides equivalent or better behavior.
4. Remove superseded fork code only after the regression test passes against the
   proposed replacement. Retain useful tests and document the superseding SHA.
5. Resolve changes on a dedicated integration branch. Validate migrations using
   saved synthetic legacy fixtures, not production secrets.
6. Run applicable syntax, unit, race, vet, native FRP validation and isolated
   multi-peer traffic/safety tests. Check CLI/WebUI parity and per-peer rollback.
7. Review the final diff and record remaining limitations. Push only to origin.
   Do not merge to the release/default branch or publish a release while critical
   acceptance cases fail or remain unverified.

## Implementation invariants

- CLI and WebUI must share normalized port/schema and lifecycle semantics.
- KCP must be the actual FRP transport; control traffic binds to the owned GRE
  address and public proxy listeners remain available.
- Configurations, registry changes and remote revisions must be validated,
  atomic, synchronized and recoverable. Never report an unacknowledged remote
  change or a merely running process as a verified connection.
- Use localized changes where possible. Explain any broader refactoring with
  evidence that separate implementations cannot preserve these invariants.

## Integration record and compatibility

- Current upstream comparison decisions, including superseding commit IDs, are
  in `docs/audit/fixes-and-regressions.md`. Consult them before reapplying patches.
- Keep one password implementation. Supported deployed hashes include legacy
  SHA-256, upstream `argon2id$` and canonical `$argon2id$`; successful migration
  must preserve accounts. Only stored random, expiring sessions authenticate.
- Upstream historical root audit documents are provenance. Current fork evidence
  and limitations live under `docs/audit/` and `docs/testing/`.
- No checksum opt-out may turn an unverified artifact into an allowed update.
- The test supervisor is not systemd. Actual installation/upgrade/reboot, legacy
  migrations, alternate carriers and incomplete security scans remain explicit
  release gates until their own evidence is recorded.
- CLI parity includes the actual interactive menu selections, not just direct
  commands. Keep secure bundle input on stdin, public setup field names correct,
  per-peer bundle selection and explicit TCP/KCP selection under regression tests.
- New upstream public-dial/carrier features must not bypass managed ownership,
  GRE-only control binding or authenticated registration/health requirements.
- CI must build the reviewed mount with VCS provenance despite runner/container
  UID differences. Trust only that checkout path; never use safe.directory=*.
  Inspect failed-step logs before attributing a CI failure to vulnerability checks.
- Carrier stop/replacement must cancel and join startup/reconnect work before
  returning. Test cleanup must stop owned workers before restoring config paths;
  retain race tests for concurrent status and immediate start/stop.
- Global legacy performance writers must refuse managed or corrupt registries
  before mutation. Do not change managed TCP multiplexing through perf.json:
  replacement requires per-peer schema, revision/ACK, migration and traffic tests.
  Keep numerical performance claims out of the UI without applicable evidence.
- Preserve verbose vulnerability evidence and distinguish reachable symbols,
  imported packages and required-module advisories. A successful scanner exit
  must not be described as certifying every package or the whole legacy surface.
