#!/bin/sh
# Copies the licences every download carries into the folder given and writes beside them the licences of the Go
# modules in the build that GOOS and GOARCH name (this machine's own build when they are unset).
set -eu

dir="$(cd "$1" && pwd)"
cd "$(dirname "$0")/.."
cp LICENSE looking-for-guidance/Media/OFL-PTSans.txt looking-for-guidance/Media/OFL-PTSerif.txt looking-for-guidance/Media/Apache-2.0-Droid.txt looking-for-guidance/Media/NOTICE-Droid.txt "$dir/"
cp looking-for-guidance/Media/NOTICE-Twemoji.txt looking-for-guidance/Media/CC-BY-4.0-Twemoji.txt "$dir/"
# The tool runs here, so it is built for this machine; the `go list` inside it reads GOOS and GOARCH.
tool="$(mktemp -d)"
trap 'rm -rf "$tool"' EXIT
GOOS= GOARCH= go build -o "$tool/notices" ./tools/notices
"$tool/notices" "$dir/third-party-notices.txt"
