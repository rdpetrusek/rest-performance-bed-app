#!/bin/bash
set -e
APK="/in/$1"; OUT=/out
jadx -d "$OUT/jadx" --show-bad-code "$APK" || true   # jadx exits nonzero on minor errors
java -jar /opt/apktool.jar d -f -o "$OUT/apktool" "$APK"
echo done
