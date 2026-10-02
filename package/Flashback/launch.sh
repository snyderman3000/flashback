#!/bin/sh
# Flashback — Anki flashcards for OnionOS
appdir=/mnt/SDCARD/App/Flashback
cd "$appdir" || exit 1
mkdir -p data /mnt/SDCARD/Anki
log="$appdir/data/stdout.log"
if [ -f "$log" ] && [ "$(wc -c < "$log")" -gt 262144 ]; then mv "$log" "$log.1"; fi

"$appdir/flashback" >> "$log" 2>&1

sync
