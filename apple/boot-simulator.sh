#!/bin/bash
# Wait for the selected simulator and its first-boot migrations.
set -euo pipefail
device=${1:?Simulator UDID required}
if xcrun simctl bootstatus "$device" -b; then exit 0; fi
# A fresh GitHub-hosted runner owns only disposable simulators.
[[ ${RUNNER_ENVIRONMENT:-} == github-hosted ]] || exit 1
xcrun simctl shutdown "$device" || true
xcrun simctl erase "$device"
xcrun simctl bootstatus "$device" -b
