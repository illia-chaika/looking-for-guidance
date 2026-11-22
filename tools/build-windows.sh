#!/bin/sh
# Builds the unsigned Windows EXE for players and the ZIP read by existing updaters.
set -eu

cd "$(dirname "$0")/.."
asset="looking-for-guidance-Windows"
version="$(awk '/^## Version:/ { print $3 }' looking-for-guidance/looking-for-guidance.toc)"
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "Version $version in looking-for-guidance.toc is not X.Y.Z; the updater would ignore it." >&2
  exit 1
fi
mkdir -p dist
# The Go linker reads the generated resource object from helper/. Serialize simultaneous builds of this target.
exec 9<dist
python3 -c 'import fcntl; fcntl.flock(9, fcntl.LOCK_EX)'
work="$(mktemp -d dist/.windows-build.XXXXXX)"
syso="helper/resource_windows_amd64.syso"
trap 'rm -rf "$work" "$syso"' EXIT
package="$work/package"
mkdir "$package"
export LC_ALL=C
export GOOS=windows GOARCH=amd64 CGO_ENABLED=0
tools/copy-notices.sh "$package"
cp NOTICE.md "$package/"
for notice in "$package"/*; do
  printf '%s\n\n' "${notice##*/}"
  cat "$notice"
  printf '\n\n'
done >"$work/Licenses.txt"
GOOS= GOARCH= go run ./tools/winres helper/windows.rc "$syso" "$work/Licenses.txt"
exe="$package/Looking for Guidance.exe"
go build -trimpath -ldflags "-s -w -H windowsgui -X main.version=$version" -o "$exe" ./helper
zip -q -X -j "$work/$asset.zip" "$package"/*
# Check the exact file name used by the updater and the notices carried by the standalone download.
unzip -p "$work/$asset.zip" 'Looking for Guidance.exe' | cmp "$exe" -
python3 - "$exe" "$work/Licenses.txt" <<'PY'
from pathlib import Path
import sys

if Path(sys.argv[2]).read_bytes() not in Path(sys.argv[1]).read_bytes():
    raise SystemExit("The release notices are missing from the Windows executable")
PY
mv -f "$exe" "dist/$asset.exe"
mv -f "$work/$asset.zip" "dist/$asset.zip"
shasum -a 256 "dist/$asset.exe" "dist/$asset.zip"
echo "Built $version for Windows (experimental, unsigned)."
