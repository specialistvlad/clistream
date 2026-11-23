#!/bin/bash
# Keep enrollment data private. Report only catalog counts and provider events.
set -eu
device=${1:?Simulator UDID required}
app=com.vlk.clistream.acceptance
group_id=group.com.vlk.clistream.acceptance.workspace
group=$(xcrun simctl get_app_container "$device" "$app" "$group_id" 2>/dev/null || true)
if [[ -z $group ]]; then
  # Xcode may shut down the device before the CI reporting step.
  data=$(xcrun simctl list devices --json | jq -r --arg id "$device" '.devices[][] | select(.udid == $id) | .dataPath')
  for metadata in "$data"/Containers/Shared/AppGroup/*/.com.apple.mobile_container_manager.metadata.plist; do
    [[ -f $metadata ]] || continue
    if [[ $(plutil -extract MCMMetadataIdentifier raw "$metadata" 2>/dev/null) == "$group_id" ]]; then
      group=${metadata%/*}
      break
    fi
  done
fi
if [[ -z $group ]]; then
  echo "App Group container unavailable"
elif [[ -f "$group/CLIStream/finder-servers.json" ]]; then
  jq -r 'length | "App Group server count \(.)"' "$group/CLIStream/finder-servers.json"
else
  echo "Files catalog missing from app group"
fi
if [[ -n $group && -f "$group/CLIStream/finder-index.plist" ]]; then
  plutil -convert json -o - "$group/CLIStream/finder-index.plist" | jq -r '.nodes | length | "Provider index item count \(.)"'
else
  echo "Provider index unavailable"
fi
xcrun simctl spawn "$device" log show --last 15m --style compact \
  --predicate 'subsystem == "com.vlk.clistream" AND category IN {"FileProvider", "Finder"}' 2>/dev/null | tail -20
