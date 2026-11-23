#!/bin/bash
set -euo pipefail
platform=$1
shift
if [[ "$platform" == macOS && -n ${CLISTREAM_MAC_DEVELOPMENT_TEAM:-} ]]; then
  set -- -allowProvisioningUpdates "DEVELOPMENT_TEAM=$CLISTREAM_MAC_DEVELOPMENT_TEAM" \
    "CODE_SIGN_IDENTITY=Apple Development" "$@"
fi
exec xcodebuild "$@"
