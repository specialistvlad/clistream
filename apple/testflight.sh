#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")"
: "${DEVELOPMENT_TEAM:?}"
build_number=${BUILD_NUMBER:-$(date -u +%y%m%d%H%M)}
auth=(-allowProvisioningUpdates)
# Archive-only validation can use the development team's local Xcode account.
if [[ ${ARCHIVE_ONLY:-0} != 1 || -n ${ASC_KEY_PATH:-} ]]; then
  : "${ASC_KEY_PATH:?set path to App Store Connect API key}" "${ASC_KEY_ID:?}" "${ASC_ISSUER_ID:?}"
  auth+=(-authenticationKeyPath "$ASC_KEY_PATH" -authenticationKeyID "$ASC_KEY_ID" -authenticationKeyIssuerID "$ASC_ISSUER_ID")
fi
platform=${1:-iOS}
case "$platform" in iOS|macOS) ;; *) echo "expected iOS or macOS" >&2; exit 2;; esac
archive="build/release/$platform-$build_number.xcarchive"
export GITHUB_ACTIONS=true
xcodebuild -quiet archive -scheme "CLIStream-$platform" -destination "generic/platform=$platform" -archivePath "$archive" -derivedDataPath build/dd "DEVELOPMENT_TEAM=$DEVELOPMENT_TEAM" "CURRENT_PROJECT_VERSION=$build_number" "${auth[@]}"
if [[ "$platform" == macOS ]]; then
  app="$archive/Products/Applications/CLIStream.app"
  extension="$app/Contents/PlugIns/CLIStreamFinder.appex"
  codesign --verify --strict "$extension"
  codesign --verify --strict "$app"
  for bundle in "$app" "$extension"; do
    entitlements=$(codesign -d --entitlements :- "$bundle" 2>/dev/null)
    if plutil -extract 'com\.apple\.developer\.fileprovider\.testing-mode' raw - <<<"$entitlements" >/dev/null 2>&1; then
      echo "A shipping bundle contains the development-only File Provider testing capability." >&2
      exit 1
    fi
    group=$(plutil -extract 'com\.apple\.security\.application-groups.0' raw - <<<"$entitlements")
    [[ "$group" == "$DEVELOPMENT_TEAM.com.vlk.clistream.workspace" ]]
    for direction in client server; do
      [[ $(plutil -extract "com\.apple\.security\.network\.$direction" raw - <<<"$entitlements") == true ]]
    done
  done
fi
if [[ ${ARCHIVE_ONLY:-0} == 1 ]]; then echo "$archive"; exit 0; fi
plist="build/release/options-$platform.plist"
cat > "$plist" <<EOF
<plist version="1.0"><dict><key>method</key><string>app-store-connect</string><key>destination</key><string>upload</string><key>signingStyle</key><string>automatic</string><key>teamID</key><string>$DEVELOPMENT_TEAM</string><key>manageAppVersionAndBuildNumber</key><false/></dict></plist>
EOF
xcodebuild -exportArchive -archivePath "$archive" -exportOptionsPlist "$plist" -exportPath "build/release/upload-$platform-$build_number" "${auth[@]}"
