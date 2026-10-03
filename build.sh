#!/bin/sh
# Builds the packages:
#   dist/Flashback-v<version>-OnionOS.zip  App/Flashback for OnionOS (Miyoo Mini Plus / Flip, ARMv7)
#   dist/Flashback-v<version>-NextUI.zip   Tools/tg5040/Flashback.pak for NextUI / NX Redux (TrimUI, arm64)
set -e
cd "$(dirname "$0")"
VERSION=$(grep -o 'const version = "[^"]*"' main.go | cut -d'"' -f2)
[ -f go.work ] || go mod tidy
go test ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build -trimpath -ldflags="-s -w" -o package/Flashback/flashback .
go run . -icon package/Flashback/icon.png
rm -rf dist/stage && mkdir -p dist/stage/App
cp -r package/Flashback dist/stage/App/
rm -rf dist/stage/App/Flashback/data
rm -f "dist/Flashback-v$VERSION-OnionOS.zip"
(cd dist/stage && zip -qr "../Flashback-v$VERSION-OnionOS.zip" App)

pak=dist/stage/Tools/tg5040/Flashback.pak
mkdir -p "$pak"
cp -r package/Flashback/DejaVuSans.ttf package/Flashback/README.txt package/Flashback/licenses package/tg5040/launch.sh "$pak/"
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o "$pak/flashback" .
rm -f "dist/Flashback-v$VERSION-NextUI.zip"
(cd dist/stage && zip -qr "../Flashback-v$VERSION-NextUI.zip" Tools)
rm -rf dist/stage
echo "Built dist/Flashback-v$VERSION-OnionOS.zip and dist/Flashback-v$VERSION-NextUI.zip"
