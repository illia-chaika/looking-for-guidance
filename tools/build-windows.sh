#!/bin/sh
# Builds the Windows download on this Mac: dist/looking-for-guidance-Windows.zip with the companion's executable,
# which carries the add-on, and the licences. Experimental: it is not signed and has never run on Windows. Nothing is
# published.
set -eu

cd "$(dirname "$0")/.."
zip="dist/looking-for-guidance-Windows.zip"
version="$(awk '/^## Version:/ { print $3 }' looking-for-guidance/looking-for-guidance.toc)"
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "Version $version in looking-for-guidance.toc is not X.Y.Z; the updater would ignore it." >&2
  exit 1
fi
work="$(mktemp -d)"
syso="helper/resource_windows_amd64.syso"
trap 'rm -rf "$work" "$syso"' EXIT
# The icon Explorer shows for the executable. The linker takes the object from the package folder by its name.
go run ./tools/winres helper/windows.rc "$syso"
export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$version" -o "$work/Looking for Guidance.exe" ./helper
tools/copy-notices.sh "$work"
# The updater takes "Looking for Guidance.exe" from the top level of the ZIP (extractApp in helper/place_windows.go).
mkdir -p dist
rm -f "$zip"
zip -q -X -j "$zip" "$work"/*
unzip -l "$zip"
shasum -a 256 "$zip"
echo "Built $version for Windows: $zip (experimental, unsigned, not published)."
