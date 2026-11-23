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
  index="$group/CLIStream/finder-index.plist"
  plutil -convert json -o - "$index" | jq -r '.nodes | length | "Provider index item count \(.)"'
  # Compare only known virtual folder identifiers against the system's view.
  # Keep the dump in the pipe. It can contain user paths and filenames.
  perl -e 'alarm 8; exec @ARGV' xcrun simctl spawn "$device" fileproviderctl dump -l "$app.files/workspaces" 2>/dev/null |
    awk -F '\t' 'FILENAME != "-" { kind[$1] = $2; next }
      { for (id in kind) if (index($0, id)) seen[kind[id]] = 1; lines++ }
      END { print "File Provider system dump lines", lines + 0;
        for (item in seen) print "System references a catalog item of kind", item }' \
      <(plutil -convert json -o - "$index" | jq -r '.nodes[] | select(.kind == "server" or .kind == "host" or .kind == "workspace") | [.id,.kind] | @tsv') -
else
  echo "Provider index unavailable"
fi
xcrun simctl spawn "$device" log show --last 15m --style compact \
  --predicate 'subsystem == "com.vlk.clistream" AND category IN {"FileProvider", "Finder"}' 2>/dev/null | tail -20
# State transitions and numeric errors from the system side of the same domain.
# Never export raw fileproviderd messages, which can include paths or item IDs.
xcrun simctl spawn "$device" log show --last 15m --style ndjson \
  --predicate 'process == "fileproviderd" AND (messageType == error OR messageType == fault)' 2>/dev/null |
  jq -Rr 'fromjson? | .eventMessage? | strings | select(test("clistream|workspaces"; "i"))
    | [scan("(?:[A-Za-z]+ErrorDomain|Code[=: ]+-?[0-9]+|invalid filename|invalid name|invalid item|not materialized|no such item)"; "i")]
    | select(length > 0) | unique | join(" ")' | sort | uniq -c | head -30
