#!/usr/bin/env bash
set -euo pipefail
cd /work
mkdir -p build/qa
for f in *.sh tests/integration/run.sh; do bash -n "$f"; done
shellcheck install.sh install-fork.sh tests/integration/run.sh
bash tests/test_legacy_units.sh
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
