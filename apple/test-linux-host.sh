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
# Docker on the hosted Mac can bind-mount the checkout inside its Linux VM.
mkdir -p "$PWD/apple/build"
stage=$(mktemp -d "$PWD/apple/build/linux-host-XXXXXXXX")
container=""
cleanup(){
  if [[ $? != 0 && -n "$container" ]]; then docker logs "$container" 2>&1 | grep '^Finder ' || true; fi
  if [[ -n "$container" ]]; then docker rm -f "$container" >/dev/null; fi
  rm -rf "$stage"
}
trap cleanup EXIT
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64) arch=arm64 ;; x86_64) arch=amd64 ;; esac
mkdir "$stage/bin"
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o "$stage/bin/" ./cmd/... ./scripts/apple-ui
# Choose a loopback port. The host and container numbers must match
# for the signed invitation and the fixture URL to work inside and outside.
port=$(swift -e 'import Foundation; import Network
let listener = try NWListener(using: .tcp, on: .any)
listener.newConnectionHandler = { $0.cancel() }
listener.stateUpdateHandler = { if case .ready = $0 { print(listener.port!.rawValue); exit(0) } }
listener.start(queue: .main); dispatchMain()')
container=$(docker run -d --read-only --cap-drop ALL --tmpfs /tmp -w /work \
  -v "$stage/bin:/work/bin:ro" \
  -e CLISTREAM_UI_BROKER_PORT="$port" -e CLISTREAM_UI_FIXTURE_PORT="$port" \
  -p "127.0.0.1:$port:$port/udp" -p "127.0.0.1:$port:$port/tcp" \
  debian:bookworm-slim /work/bin/apple-ui sh -ec \
  'umask 077; env | grep "^TEST_RUNNER_CLISTREAM_" > /tmp/fixture.tmp && mv /tmp/fixture.tmp /tmp/fixture.env && exec sleep infinity')
# Read the completed fixture environment from its tmpfs. A capability-free
# container cannot assume write permission on directories owned by the Mac UID.
for ((i=0; i<240; i++)); do
  if docker cp "$container:/tmp/fixture.env" "$stage/env" >/dev/null 2>&1; then break; fi
  [[ $(docker inspect --format '{{.State.Running}}' "$container") == true ]] || break
  sleep .5
done
if [[ ! -s "$stage/env" ]]; then
  docker inspect --format 'Linux fixture status {{.State.Status}}, exit code {{.State.ExitCode}}' "$container" >&2
  exit 1
fi
while IFS= read -r entry; do export "$entry"; done < "$stage/env"
result=$(mktemp -d "$PWD/apple/build/results-linux-ui-XXXXXXXX")
GITHUB_ACTIONS=true apple/acceptance-xcodebuild.sh "$platform" -quiet -project apple/CLIStream.xcodeproj \
  -scheme "CLIStream-$platform" -destination "$destination" \
  -derivedDataPath "apple/build/$dd" CLISTREAM_APP_ID=com.vlk.clistream.acceptance \
  -parallel-testing-enabled NO -collect-test-diagnostics never -resultBundlePath "$result/native.xcresult" \
  "-only-testing:$target/NativeSessionTests/$testCase" test
go run ./scripts/apple-report "$result/native.xcresult" | tee "$result/cases.json"
jq -e --arg name "NativeSessionTests/$testCase()" 'any(.cases[]; .test == $name and .result == "Passed")' "$result/cases.json" >/dev/null
