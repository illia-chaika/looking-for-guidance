#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
build_dir="dist/Looking for Guidance.app/Contents"
work="$(mktemp -d)"
iconset="$work/AppIcon.iconset"
trap 'rm -rf "$work"' EXIT
mkdir -p "$build_dir/MacOS" "$build_dir/Resources" "$iconset"
version="$(awk '/^## Version:/ { print $3 }' looking-for-guidance/looking-for-guidance.toc)"
# Without an explicit target cgo builds for the SDK's macOS, which older systems refuse to run.
min_macos="$(plutil -extract LSMinimumSystemVersion raw tools/Info.plist)"
export MACOSX_DEPLOYMENT_TARGET="$min_macos"
export CGO_CFLAGS="-O2 -g -mmacosx-version-min=$min_macos"
export CGO_LDFLAGS="-O2 -g -mmacosx-version-min=$min_macos"
for arch in arm64 amd64; do
  GOARCH="$arch" CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$work/LookingForGuidance-$arch" ./helper
done
lipo -create -output "$build_dir/MacOS/LookingForGuidance" "$work/LookingForGuidance-arm64" "$work/LookingForGuidance-amd64"
for arch in arm64 x86_64; do
  minos="$(vtool -arch "$arch" -show-build "$build_dir/MacOS/LookingForGuidance" | awk '$1 == "minos" { print $2 }')"
  if [ "$minos" != "$min_macos" ]; then
    echo "The $arch build needs macOS $minos, but Info.plist promises $min_macos." >&2
    exit 1
  fi
done
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" helper/assets/app-icon.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
  retina=$((size * 2))
  sips -z "$retina" "$retina" helper/assets/app-icon.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$build_dir/Resources/AppIcon.icns"
tools/copy-notices.sh "$build_dir/Resources"
python3 - "$build_dir/Resources" <<'PY'
import hashlib, json, sys, zipfile
from pathlib import Path
addon = Path("looking-for-guidance")
resources = Path(sys.argv[1])
files = {}
with zipfile.ZipFile(resources / "addon-payload.zip", "w", zipfile.ZIP_DEFLATED) as package:
    for p in sorted(addon.rglob("*")):
        relative = p.relative_to(addon)
        if not p.is_file() or any(part.startswith(".") for part in relative.parts):
            continue
        data = p.read_bytes()
        package.writestr(relative.as_posix(), data)
        files[relative.as_posix()] = hashlib.sha256(data).hexdigest()
(resources / "addon-files.json").write_text(json.dumps(files, indent=2) + "\n")
PY
cp tools/Info.plist "$build_dir/Info.plist"
plutil -replace CFBundleShortVersionString -string "${version%%-*}" "$build_dir/Info.plist"
plutil -replace CFBundleVersion -string "${version%%-*}" "$build_dir/Info.plist"
plutil -lint "$build_dir/Info.plist"
if [ -n "${LFG_SIGN_IDENTITY:-}" ]; then
  codesign --force --options runtime --timestamp --sign "$LFG_SIGN_IDENTITY" "dist/Looking for Guidance.app"
else
  codesign --force --sign - "dist/Looking for Guidance.app"
fi
codesign --verify --deep --strict "dist/Looking for Guidance.app"
