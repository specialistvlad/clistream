#!/bin/bash
# Run the Apple UI against a Linux daemon and Linux child in a private container.
set -euo pipefail
cd "$(dirname "$0")/.."
case "${1:-iOS}" in
  iOS)
    : "${SIMULATOR_ID:?Set an OS 27 simulator UDID}"
    platform=iOS
    destination="platform=iOS Simulator,id=$SIMULATOR_ID"
    target=CLIStream-UITests
    testCase=testPairPreviewAndSend
    dd=accept-ios ;;
  macOS)
    export CLISTREAM_FINDER_ACCEPTANCE=1
    platform=macOS
    destination="platform=macOS,arch=$(uname -m)"
    target=CLIStream-MacUITests
    testCase=testFinderWorkspaceRoundTrip
    dd=accept-mac ;;
  *) echo "expected iOS or macOS" >&2; exit 2 ;;
esac
stage=$(mktemp -d /tmp/cs-linux-ui.XXXXXX)
container=""
cleanup(){
  if [[ $? != 0 && -n "$container" ]]; then docker logs "$container" 2>&1 | grep '^Finder ' || true; fi
  if [[ -n "$container" ]]; then docker rm -f "$container" >/dev/null; fi
  rm -rf "$stage"
}
trap cleanup EXIT
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64) arch=arm64 ;; x86_64) arch=amd64 ;; esac
mkdir "$stage/bin" "$stage/state"
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o "$stage/bin/" ./cmd/... ./scripts/apple-ui
# Choose a loopback port. The host and container numbers must match
# for the signed invitation and the fixture URL to work inside and outside.
port=$(swift -e 'import Foundation; import Network
let listener = try NWListener(using: .tcp, on: .any)
listener.newConnectionHandler = { $0.cancel() }
listener.stateUpdateHandler = { if case .ready = $0 { print(listener.port!.rawValue); exit(0) } }
listener.start(queue: .main); dispatchMain()')
container=$(docker run -d --rm --read-only --cap-drop ALL --tmpfs /tmp -w /work \
  -v "$stage/bin:/work/bin:ro" -v "$stage/state:/state" \
  -e CLISTREAM_UI_BROKER_PORT="$port" -e CLISTREAM_UI_FIXTURE_PORT="$port" \
  -p "127.0.0.1:$port:$port/udp" -p "127.0.0.1:$port:$port/tcp" \
  debian:bookworm-slim /work/bin/apple-ui sh -ec \
  'env | grep "^TEST_RUNNER_CLISTREAM_" > /state/env.tmp && mv /state/env.tmp /state/env && exec sleep infinity')
for ((i=0; i<200; i++)); do [[ -f "$stage/state/env" ]] && break; sleep .1; done
[[ -f "$stage/state/env" ]] || { docker logs "$container"; exit 1; }
while IFS= read -r entry; do export "$entry"; done < "$stage/state/env"
result=$(mktemp -d "$PWD/apple/build/results-linux-ui-XXXXXXXX")
GITHUB_ACTIONS=true apple/acceptance-xcodebuild.sh "$platform" -quiet -project apple/CLIStream.xcodeproj \
  -scheme "CLIStream-$platform" -destination "$destination" \
  -derivedDataPath "apple/build/$dd" CLISTREAM_APP_ID=com.vlk.clistream.acceptance \
  -parallel-testing-enabled NO -collect-test-diagnostics never -resultBundlePath "$result/native.xcresult" \
  "-only-testing:$target/NativeSessionTests/$testCase" test
go run ./scripts/apple-report "$result/native.xcresult" | tee "$result/cases.json"
jq -e --arg name "$testCase()" 'any(.cases[]; .test == $name and .result == "Passed")' "$result/cases.json" >/dev/null
