#!/bin/sh
# Makes the Finder disk image and the ZIP used by the updater from the same notarized app.
# Needs dmgbuild, a "Developer ID Application" identity in the keychain and a notarytool profile made with
# `xcrun notarytool store-credentials`. LFG_SIGN_IDENTITY picks the identity when there is more than one;
# LFG_NOTARY_PROFILE names the profile (default lfg-notary). Nothing is published.
set -eu

cd "$(dirname "$0")/.."
app="dist/Looking for Guidance.app"
zip="dist/looking-for-guidance-macOS.zip"
dmg="dist/looking-for-guidance-macOS.dmg"
profile="${LFG_NOTARY_PROFILE:-lfg-notary}"
version="$(awk '/^## Version:/ { print $3 }' looking-for-guidance/looking-for-guidance.toc)"
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "Version $version in looking-for-guidance.toc is not X.Y.Z; the updater would ignore it." >&2
  exit 1
fi

if ! command -v dmgbuild >/dev/null 2>&1; then
  echo "Install dmgbuild to create the macOS download (python3 -m pip install dmgbuild)." >&2
  exit 1
fi

identity="${LFG_SIGN_IDENTITY:-}"
if [ -z "$identity" ]; then
  identity="$(security find-identity -v -p codesigning | awk '/"Developer ID Application: / { print $2 }' | sort -u)"
  count="$(printf '%s' "$identity" | grep -c . || true)"
  if [ "$count" != 1 ]; then
    echo "Found $count Developer ID Application identities; set LFG_SIGN_IDENTITY to the one to use." >&2
    exit 1
  fi
fi

LFG_SIGN_IDENTITY="$identity" ./tools/build-macos.sh
details="$(codesign -dvv "$app" 2>&1)"
case "$details" in
  *"flags=0x10000(runtime)"*) ;;
  *) echo "$app is not signed with the hardened runtime." >&2; exit 1 ;;
esac
case "$details" in
  *"TeamIdentifier=not set"*) echo "$app has no team identifier." >&2; exit 1 ;;
esac

tmp="$(mktemp -d)"
mount="$tmp/mount"
attached=false
cleanup() {
  if [ "$attached" = true ]; then hdiutil detach "$mount" >/dev/null || return; fi
  rm -rf "$tmp"
}
trap cleanup EXIT

notarize() {
  xcrun notarytool submit "$1" --keychain-profile "$profile" --wait --timeout 30m --output-format json >"$tmp/submission.json"
  # notarytool can exit successfully when Apple rejects the submission.
  set -- $(python3 -c 'import json, sys; r = json.load(open(sys.argv[1])); print(r.get("status") or "-", r.get("id") or "-")' "$tmp/submission.json")
  if [ "$1" != Accepted ]; then
    echo "Apple's notary service answered $1. Details: xcrun notarytool log $2 --keychain-profile $profile" >&2
    exit 1
  fi
}

ditto -c -k --keepParent "$app" "$tmp/notarize.zip"
notarize "$tmp/notarize.zip"
xcrun stapler staple "$app"
xcrun stapler validate "$app"
spctl -a -vv -t exec "$app"
ditto -c -k --keepParent "$app" "$tmp/update.zip"

cp tools/macos-dmg-background@2x.png "$tmp/background@2x.png"
sips -z 300 660 "$tmp/background@2x.png" --out "$tmp/background.png" >/dev/null
dmgbuild -s tools/macos-dmg.py -D app="$app" -D background="$tmp/background.png" \
  "Looking for Guidance" "$tmp/download.dmg"
codesign --timestamp --sign "$identity" --identifier io.github.illia-chaika.looking-for-guidance.dmg "$tmp/download.dmg"
notarize "$tmp/download.dmg"
xcrun stapler staple "$tmp/download.dmg"
xcrun stapler validate "$tmp/download.dmg"
spctl -a -vv -t open --context context:primary-signature "$tmp/download.dmg"
hdiutil verify "$tmp/download.dmg" >/dev/null

mkdir "$mount"
hdiutil attach -readonly -noautoopen -nobrowse -mountpoint "$mount" "$tmp/download.dmg" >/dev/null
attached=true
[ "$(readlink "$mount/Applications")" = /Applications ]
codesign --verify --deep --strict "$mount/Looking for Guidance.app"
xcrun stapler validate "$mount/Looking for Guidance.app"
hdiutil detach "$mount" >/dev/null
attached=false

mv -f "$tmp/update.zip" "$zip"
mv -f "$tmp/download.dmg" "$dmg"
shasum -a 256 "$zip" "$dmg"
echo "Notarized $version: $zip and $dmg (not published)."
