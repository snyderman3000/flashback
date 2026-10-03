#!/bin/sh
# Flashback — Anki flashcards for NextUI / NX Redux (TrimUI Brick, Brick Pro, Smart Pro)
cd "$(dirname "$0")" || exit 1
appdir=$(pwd)
mkdir -p data /mnt/SDCARD/Anki
log="$appdir/data/stdout.log"
if [ -f "$log" ] && [ "$(wc -c < "$log")" -gt 262144 ]; then mv "$log" "$log.1"; fi

FLASHBACK_PLATFORM=tg5040 "$appdir/flashback" >> "$log" 2>&1

sync
