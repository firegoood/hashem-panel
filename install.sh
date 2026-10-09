#!/usr/bin/env bash
# Maintained fork: install from a reviewed checkout; never execute remote main.
set -euo pipefail
repo_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
exec bash "$repo_dir/install-fork.sh" "$@"
