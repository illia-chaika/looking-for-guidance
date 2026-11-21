#!/bin/sh
# Builds the app, signs it with Developer ID, has Apple notarize it and packs the ZIP the updater downloads.
# Needs a "Developer ID Application" identity in the keychain and a notarytool profile made with
# `xcrun notarytool store-credentials`. LFG_SIGN_IDENTITY picks the identity when there is more than one;
# LFG_NOTARY_PROFILE names the profile (default lfg-notary). Nothing is published.
set -eu

cd "$(dirname "$0")/.."
app="dist/Looking for Guidance.app"
zip="dist/looking-for-guidance-macOS.zip"
profile="${LFG_NOTARY_PROFILE:-lfg-notary}"
version="$(awk '/^## Version:/ { print $3 }' looking-for-guidance/looking-for-guidance.toc)"
if ! printf '%s\n' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
  echo "Version $version in looking-for-guidance.toc is not X.Y.Z; the updater would ignore it." >&2
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

rm -f "$zip"
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
trap 'rm -rf "$tmp"' EXIT
ditto -c -k --keepParent "$app" "$tmp/notarize.zip"
xcrun notarytool submit "$tmp/notarize.zip" --keychain-profile "$profile" --wait --timeout 30m --output-format json >"$tmp/submission.json"
# notarytool can finish without an error while Apple rejected the app, so the answer itself decides.
set -- $(python3 -c 'import json, sys; r = json.load(open(sys.argv[1])); print(r.get("status") or "-", r.get("id") or "-")' "$tmp/submission.json")
if [ "$1" != Accepted ]; then
  echo "Apple's notary service answered $1. Details: xcrun notarytool log $2 --keychain-profile $profile" >&2
  exit 1
fi

xcrun stapler staple "$app"
xcrun stapler validate "$app"
spctl -a -vv -t exec "$app"
ditto -c -k --keepParent "$app" "$zip"
shasum -a 256 "$zip"
echo "Notarized $version: $zip (not published)."
