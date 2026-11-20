#!/bin/bash
set -euo pipefail
cd "$(dirname "$0")/.."
out="$PWD/apple/build"
mkdir -p "$out/headers"
cp apple/Bridge/Core.h "$out/headers"
build(){
 local sdk=$1 os=$2 arch=$3 target=$4
 local clang root
 clang=$(xcrun --sdk "$sdk" -f clang)
 root=$(xcrun --sdk "$sdk" --show-sdk-path)
 GOOS="$os" GOARCH="$arch" CGO_ENABLED=1 CC="$clang -isysroot $root -target $target" go build -trimpath -buildmode=c-archive -o "$out/$sdk-$arch.a" ./apple/Bridge
}
build iphoneos ios arm64 arm64-apple-ios17.0
build iphonesimulator ios arm64 arm64-apple-ios17.0-simulator
build iphonesimulator ios amd64 x86_64-apple-ios17.0-simulator
build macosx darwin arm64 arm64-apple-macos14.0
build macosx darwin amd64 x86_64-apple-macos14.0
for sdk in iphonesimulator macosx; do
 lipo -create "$out/$sdk-arm64.a" "$out/$sdk-amd64.a" -output "$out/$sdk.a"
done
rm -rf "$out/Core.xcframework"
xcodebuild -create-xcframework -library "$out/iphoneos-arm64.a" -headers "$out/headers" -library "$out/iphonesimulator.a" -headers "$out/headers" -library "$out/macosx.a" -headers "$out/headers" -output "$out/Core.xcframework"

