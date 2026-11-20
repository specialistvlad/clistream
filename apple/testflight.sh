#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
: "${ASC_KEY_PATH:?set path to App Store Connect API key}" "${ASC_KEY_ID:?}" "${ASC_ISSUER_ID:?}" "${DEVELOPMENT_TEAM:?}"
build_number=${BUILD_NUMBER:-$(date -u +%y%m%d%H%M)}
auth=(-allowProvisioningUpdates -authenticationKeyPath "$ASC_KEY_PATH" -authenticationKeyID "$ASC_KEY_ID" -authenticationKeyIssuerID "$ASC_ISSUER_ID")
platform=${1:-iOS}
case "$platform" in iOS|macOS) ;; *) echo "expected iOS or macOS" >&2; exit 2;; esac
archive="build/release/$platform-$build_number.xcarchive"
export GITHUB_ACTIONS=true
xcodebuild -quiet archive -scheme "CLIStream-$platform" -destination "generic/platform=$platform" -archivePath "$archive" -derivedDataPath build/dd "DEVELOPMENT_TEAM=$DEVELOPMENT_TEAM" "CURRENT_PROJECT_VERSION=$build_number" "${auth[@]}"
plist="build/release/options-$platform.plist"
cat > "$plist" <<EOF
<plist version="1.0"><dict><key>method</key><string>app-store-connect</string><key>destination</key><string>upload</string><key>signingStyle</key><string>automatic</string><key>teamID</key><string>$DEVELOPMENT_TEAM</string><key>manageAppVersionAndBuildNumber</key><false/></dict></plist>
EOF
xcodebuild -exportArchive -archivePath "$archive" -exportOptionsPlist "$plist" -exportPath "build/release/upload-$platform-$build_number" "${auth[@]}"

