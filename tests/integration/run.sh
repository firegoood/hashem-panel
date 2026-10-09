#!/usr/bin/env bash
set -euo pipefail
cd /work
mkdir -p build/qa
# actions/checkout is owned by the runner, whereas Docker runs as root. Trust
# only this reviewed mount so Go can obtain VCS provenance when building it.
git config --global --add safe.directory /work
for f in *.sh tests/*.sh tests/integration/run.sh; do bash -n "$f"; done
shellcheck install.sh install-fork.sh tests/integration/run.sh
bash tests/test_legacy_units.sh
bash tests/test_backhaul_schema.sh
bash tests/test_wss_front.sh
bash tests/test_dial_route.sh
python3 tests/test_managed_menu.py
python3 tests/test_perf_mux.py
cd panel
test -z "$(gofmt -l ./*.go)"
go test -count=1 ./...
go vet ./...
go test -race -count=1 ./...
go build -o /work/build/qa/gre-panel .
install -m 755 /work/build/qa/gre-panel /usr/local/bin/gre-panel
cd /work
chmod +x tests/integration/systemctl tests/integration/echo.py
python3 tests/integration/multipeer.py
