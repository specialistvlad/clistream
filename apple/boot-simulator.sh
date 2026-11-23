#!/bin/bash
# Wait for the selected simulator and its first-boot migrations.
set -euo pipefail
device=${1:?Simulator UDID required}
transcript=$(mktemp)
trap 'rm -f "$transcript"' EXIT
boot() {
  # Some runtimes return zero with a terminal "Data Migration Failed" status.
  xcrun simctl bootstatus "$device" -b 2>&1 | tee "$transcript" &&
    ! grep -Fq 'Data Migration Failed' "$transcript"
}
if boot; then exit 0; fi
# A fresh GitHub-hosted runner owns only disposable simulators.
[[ ${RUNNER_ENVIRONMENT:-} == github-hosted ]] || exit 1
xcrun simctl shutdown "$device" || true
xcrun simctl erase "$device"
boot
