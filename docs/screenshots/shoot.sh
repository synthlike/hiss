#!/bin/sh
# Regenerates the README's screenshots: internal/ui/shots_test.go draws each
# scene with the real editor as an HTML terminal window, in both themes, and
# headless Chrome takes it. Set CHROME to use another Chrome or Chromium.
set -eu
cd "$(dirname "$0")/../.."
chrome=${CHROME:-"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"}
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

for theme in dark light; do
	go test -tags shots -run TestShots ./internal/ui -args -out "$tmp" -theme "$theme"
done
# The window fits the 104x32 grid of shots_test.go; the PNGs are at 2x.
for f in "$tmp"/*.html; do
	"$chrome" --headless=new --disable-gpu --hide-scrollbars --user-data-dir="$tmp/chrome" \
		--force-device-scale-factor=2 --window-size=1039,774 \
		--screenshot="docs/screenshots/$(basename "$f" .html).png" "file://$f" >/dev/null 2>&1
done
ls docs/screenshots/*.png
