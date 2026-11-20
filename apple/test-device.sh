#!/bin/bash
# Run a real pairing and terminal transaction on the selected simulator.
set -euo pipefail
cd "$(dirname "$0")/.."
platform=${1:-iOS}
if [[ "$platform" == macOS ]]; then dest='platform=macOS'; else
 : "${SIMULATOR_ID:?set an installed iOS simulator UDID}"
 dest="platform=iOS Simulator,id=$SIMULATOR_ID"
fi
dd="build/test-$platform-${SIMULATOR_ID:-host}"
export GITHUB_ACTIONS=true
bin="$PWD/bin"
make build
cd apple
xcodebuild -quiet build-for-testing -scheme "CLIStream-$platform" -destination "$dest" -derivedDataPath "$dd"
if [[ "$platform" == iOS ]]; then xcrun simctl bootstatus "$SIMULATOR_ID" -b; fi
state=$(mktemp -d /tmp/clistream-ui.XXXXXXXX)
daemon_pid= supervisor_pid= viewer_pid=
cleanup() {
 [[ -z "$viewer_pid" ]] || kill "$viewer_pid" 2>/dev/null || true
 [[ -z "$supervisor_pid" ]] || kill "$supervisor_pid" 2>/dev/null || true
 [[ -z "$daemon_pid" ]] || kill "$daemon_pid" 2>/dev/null || true
 wait || true
 rm -rf "$state"
}
trap cleanup EXIT
port=$(python3 -c 'import socket;s=socket.socket(type=socket.SOCK_DGRAM);s.bind(("127.0.0.1",0));print(s.getsockname()[1])')
"$bin/clistreamd" -listen "127.0.0.1:$port" -state "$state/server" 2>"$state/server.log" &
daemon_pid=$!
for i in {1..100}; do [[ -S "$state/server/admin.sock" ]] && break; sleep .05; done
"$bin/clistreamd" -state "$state/server" -qr=false invite supervisor | "$bin/clistream-supervise" -state "$state/sup" -pair
mkdir "$state/work"
touch "$state/work/fixture.txt"
(cd "$state/work" && exec "$bin/clistream-supervise" -state "$state/sup" -local-input=false -- /bin/cat) >"$state/output" 2>"$state/session" &
supervisor_pid=$!
"$bin/clistreamd" -state "$state/server" -qr=false invite client | "$bin/clistream" -state "$state/client" pair
sid=$("$bin/clistream" -state "$state/client" list | python3 -c 'import json,sys;print(json.load(sys.stdin)[0]["ID"])')
"$bin/clistream" -state "$state/client" attach "$sid" </dev/null >"$state/viewer" &
viewer_pid=$!
export TEST_RUNNER_CLISTREAM_SESSION="$sid"
export TEST_RUNNER_CLISTREAM_LINK
TEST_RUNNER_CLISTREAM_LINK=$("$bin/clistreamd" -state "$state/server" -qr=false invite client)
xcodebuild -quiet test-without-building -scheme "CLIStream-$platform" -parallel-testing-enabled NO -destination "$dest" -derivedDataPath "$dd"
grep -q apple-client-echo "$state/output"

grep -q apple-client-echo "$state/viewer"
