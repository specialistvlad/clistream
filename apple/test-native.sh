#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
platform=${1:-iOS}
case "$platform" in
  macOS)
    export CLISTREAM_MAC_BOARD=1
    destination="platform=macOS,arch=$(uname -m)"
    buildDestination="generic/platform=macOS"
    dd=accept-mac
    hosted=CLIStream-MacTests
    suite=CLIStream-MacUITests ;;
  iOS)
    : "${SIMULATOR_ID:?Set SIMULATOR_ID to an iOS 27 or later simulator UDID}"
    destination="platform=iOS Simulator,id=$SIMULATOR_ID"
    buildDestination="generic/platform=iOS Simulator"
    dd=accept-ios
    hosted=CLIStream-iOSTests
    suite=CLIStream-UITests ;;
  *) echo "expected macOS or iOS" >&2; exit 2 ;;
esac
tests=("-only-testing:$hosted" "-only-testing:$suite/SessionTests" "-only-testing:$suite/NativeSessionTests")
if [[ $platform == iOS ]] && [[ $(xcrun simctl list devices --json | jq -r --arg id "$SIMULATOR_ID" '.devices[][] | select(.udid == $id) | .deviceTypeIdentifier') == *iPhone-Duo ]]; then
  export CLISTREAM_DUO_NAME=$(xcrun simctl list devices --json | jq -r --arg id "$SIMULATOR_ID" '.devices[][] | select(.udid == $id) | .name')
  tests+=("-only-testing:$suite/DuoContinuityTests")
fi
if [[ -n ${CLISTREAM_TEST_CASES:-} ]]; then
  tests=()
  IFS=, read -ra cases <<< "$CLISTREAM_TEST_CASES"
  for test_case in "${cases[@]}"; do
    if [[ ! $test_case =~ ^(ClientTests|NativeSessionTests|SessionTests|DuoContinuityTests)/test[A-Za-z0-9]+$ ]]; then
      echo "Expected Class/testMethod for each selected XCTest case." >&2
      exit 2
    fi
    target=$suite
    if [[ $test_case == ClientTests/* ]]; then target=$hosted; fi
    tests+=("-only-testing:$target/$test_case")
  done
fi
if [[ $platform == macOS && ( -z ${CLISTREAM_TEST_CASES:-} || $CLISTREAM_TEST_CASES == *testFinderWorkspaceRoundTrip* ) ]]; then
  export CLISTREAM_FINDER_ACCEPTANCE=1
fi
make build
# Compile before booting iOS. Simulator services and the Swift compiler otherwise
# compete for memory on a cold hosted Mac.
result=$(mktemp -d "$PWD/apple/build/results-$platform-XXXXXXXX")
(
  cd apple
  buildAction=(build-for-testing)
  if [[ $platform == iOS ]]; then
    buildAction=(-testProductsPath "$result/CLIStream.xctestproducts" build-for-testing)
  fi
  ./acceptance-xcodebuild.sh "$platform" -quiet -scheme "CLIStream-$platform" -derivedDataPath "build/$dd" \
    -destination "$buildDestination" -jobs 3 CLISTREAM_APP_ID=com.vlk.clistream.acceptance \
    "${buildAction[@]}"
)
if [[ $platform == iOS ]]; then
  apple/boot-simulator.sh "$SIMULATOR_ID"
  testPlan=(-testProductsPath "$result/CLIStream.xctestproducts")
else
  # Packaged test products create a second copy of the app, with the same
  # extension identifier. Run this exact Mac build against its embedded provider.
  products="$PWD/apple/build/$dd/Build/Products"
  sdkVersion=$(xcrun --sdk macosx --show-sdk-version)
  plans=("$products"/CLIStream-macOS_macosx"$sdkVersion"-*.xctestrun)
  [[ ${#plans[@]} -eq 1 && -f ${plans[0]} ]]
  testPlan=(-xctestrun "${plans[0]}")
fi
export GITHUB_ACTIONS=true CLISTREAM_HOSTED_TESTS=1
run_tests() {
  go run ./scripts/apple-ui xcodebuild -quiet "${testPlan[@]}" -destination "$destination" \
    -test-timeouts-enabled YES -default-test-execution-time-allowance 600 -maximum-test-execution-time-allowance 600 \
    -parallel-testing-enabled NO -collect-test-diagnostics never -resultBundlePath "$result/native.xcresult" \
    "${tests[@]}" -skip-testing:"$suite/NativeSessionTests/testConcurrentViewers" test-without-building
}
if ! run_tests; then
  # A cold simulator can lose its installation service or fail to initialize
  # XCTest accessibility. Reboot once only if no UI case started.
  [[ $platform == iOS && -n ${GITHUB_RUN_ID:-} ]] || exit 1
  go run ./scripts/apple-report "$result/native.xcresult" > "$result/first-attempt.json"
  jq -e '
    def runnerInitialization:
      (.test | startswith("CLIStreamUITests-Runner")) and .result == "Failed" and
      ((.failureKinds // []) |
        index("timed out waiting for ax loaded notification") != null or
        (index("failed to install or launch") != null and index("(ipc/mig) server died") != null));
    def missingRuntime:
      (.test == "CLIStream encountered an error" or .test == "CLIStreamUITests-Runner encountered an error") and
      .result == "Failed" and ((.failureKinds // []) | index("runtime path not found") != null);
    (any(.cases[]; runnerInitialization) and
      all(.cases[]; runnerInitialization or
        ((.test | startswith("ClientTests/")) and (.result == "Passed" or .result == "Skipped")))) or
      (.cases | length > 0 and all(.[]; missingRuntime))
  ' "$result/first-attempt.json" >/dev/null || exit 1
  mv "$result/native.xcresult" "$result/runner-initialization.xcresult"
  echo "Restarting the isolated simulator after a test runner initialization failure."
  if ! xcrun simctl shutdown "$SIMULATOR_ID"; then
    xcrun simctl list devices --json |
      jq -e --arg id "$SIMULATOR_ID" 'any(.devices[][]; .udid == $id and .state == "Shutdown")' >/dev/null
  fi
  apple/boot-simulator.sh "$SIMULATOR_ID"
  run_tests
fi
go run ./scripts/apple-report "$result/native.xcresult" | tee "$result/cases.json"
if [[ -n ${CLISTREAM_TEST_CASES:-} ]]; then
  for test_case in "${cases[@]}"; do
    jq -e --arg name "$test_case()" 'any(.cases[]; .test == $name and .result == "Passed")' "$result/cases.json" >/dev/null ||
      { echo "No passing result for $test_case" >&2; exit 1; }
  done
fi
case "$platform" in
  macOS) notice="Debug/CLIStream.app/Contents/Resources/LICENSE" ;;
  iOS) notice="Debug-iphonesimulator/CLIStream.app/LICENSE" ;;
esac
cmp LICENSE "apple/build/$dd/Build/Products/$notice"
