#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# Exercise the installer's actual fetch function without activating a service.
# shellcheck disable=SC1090
source <(sed -n '/^fetch_verified_artifact()/,/^}/p' install-fork.sh)
test_dir=$(mktemp -d)
trap 'rm -rf -- "$test_dir"' EXIT
mkdir "$test_dir/cache" "$test_dir/stage"
export HASHEM_INSTALL_ARTIFACT_DIR="$test_dir/cache"
printf 'synthetic verified artifact\n' > "$test_dir/cache/artifact.tgz"
digest=$(sha256sum "$test_dir/cache/artifact.tgz" | cut -d ' ' -f 1)
fetch_verified_artifact https://invalid.example/artifact.tgz artifact.tgz "$test_dir/stage/valid" "$digest"
cmp "$test_dir/cache/artifact.tgz" "$test_dir/stage/valid"
# Checking the private copy also keeps a later cache replacement from changing
# the already verified bytes that the installer extracts.
printf 'tampered\n' > "$test_dir/cache/artifact.tgz"
test "$(sha256sum "$test_dir/stage/valid" | cut -d ' ' -f 1)" = "$digest"
if fetch_verified_artifact https://invalid.example/artifact.tgz artifact.tgz "$test_dir/stage/tampered" "$digest"; then
    echo 'Tampered artifact accepted' >&2; exit 1
fi
if fetch_verified_artifact https://invalid.example/missing.tgz missing.tgz "$test_dir/stage/missing" "$digest"; then
    echo 'Missing artifact accepted' >&2; exit 1
fi
ln -s artifact.tgz "$test_dir/cache/symlink.tgz"
if fetch_verified_artifact https://invalid.example/symlink.tgz symlink.tgz "$test_dir/stage/symlink" "$digest"; then
    echo 'Symlink artifact accepted' >&2; exit 1
fi
if HASHEM_INSTALL_ARTIFACT_DIR=relative fetch_verified_artifact https://invalid.example/artifact.tgz artifact.tgz "$test_dir/stage/relative" "$digest"; then
    echo 'Relative artifact directory accepted' >&2; exit 1
fi
echo 'Verified offline artifact acceptance/rejection: PASS'
