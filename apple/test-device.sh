#!/bin/bash
# Preserve the device runner entry point for existing automation.
set -euo pipefail
exec "$(dirname "$0")/test-native.sh" "$@"
