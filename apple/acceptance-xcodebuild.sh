#!/bin/bash
set -euo pipefail
platform=$1
shift
if [[ "$platform" == macOS && -n ${CLISTREAM_MAC_DEVELOPMENT_TEAM:-} ]]; then
  set -- -allowProvisioningUpdates "DEVELOPMENT_TEAM=$CLISTREAM_MAC_DEVELOPMENT_TEAM" \
    "CODE_SIGN_IDENTITY=Apple Development" "$@"
fi
if [[ "$platform" == macOS && ${CLISTREAM_FINDER_ACCEPTANCE:-0} == 1 && -n ${CLISTREAM_MAC_DEVELOPMENT_TEAM:-} ]]; then
  # File Provider's documented testing mode enables a new domain on an
  # unattended runner. Keep this entitlement out of both shipping bundles.
  signing=$(mktemp -d "${TMPDIR:-/tmp}/clistream-finder-signing.XXXXXX")
  trap 'rm -rf "$signing"' EXIT
  cp "$(dirname "$0")/Mac.entitlements" "$signing/Acceptance.entitlements"
  /usr/libexec/PlistBuddy -c 'Add :com.apple.developer.fileprovider.testing-mode bool true' "$signing/Acceptance.entitlements"
  xcodebuild "CLISTREAM_MAC_ENTITLEMENTS=$signing/Acceptance.entitlements" "$@"
  exit
fi
exec xcodebuild "$@"
