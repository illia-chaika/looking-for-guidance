#!/bin/sh
set -eu

cd "$(dirname "$0")/.."
build_dir="dist/Looking for Guidance.app/Contents"
icon_tmp="$(mktemp -d)"
iconset="$icon_tmp/AppIcon.iconset"
trap 'rm -rf "$icon_tmp"' EXIT
mkdir -p "$build_dir/MacOS" "$build_dir/Resources" "$iconset"
version="$(awk '/^## Version:/ { print $3 }' looking-for-guidance/looking-for-guidance.toc)"
CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=$version" -o "$build_dir/MacOS/LookingForGuidance" ./helper
for size in 16 32 128 256 512; do
  sips -z "$size" "$size" helper/assets/app-icon.png --out "$iconset/icon_${size}x${size}.png" >/dev/null
  retina=$((size * 2))
  sips -z "$retina" "$retina" helper/assets/app-icon.png --out "$iconset/icon_${size}x${size}@2x.png" >/dev/null
done
iconutil -c icns "$iconset" -o "$build_dir/Resources/AppIcon.icns"
cp LICENSE looking-for-guidance/Media/OFL-PTSans.txt looking-for-guidance/Media/OFL-PTSerif.txt looking-for-guidance/Media/Apache-2.0-Droid.txt looking-for-guidance/Media/NOTICE-Droid.txt "$build_dir/Resources/"
cp looking-for-guidance/Media/NOTICE-Twemoji.txt looking-for-guidance/Media/CC-BY-4.0-Twemoji.txt "$build_dir/Resources/"
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
go run ./tools/notices "$build_dir/Resources/third-party-notices.txt"
cp tools/Info.plist "$build_dir/Info.plist"
plutil -replace CFBundleShortVersionString -string "${version%%-*}" "$build_dir/Info.plist"
plutil -lint "$build_dir/Info.plist"
codesign --force --sign - "dist/Looking for Guidance.app"
codesign --verify --deep --strict "dist/Looking for Guidance.app"
