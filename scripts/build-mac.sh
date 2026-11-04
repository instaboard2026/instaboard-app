#!/usr/bin/env bash
# Build a Mac zip: one Instaboard binary (UI + API) for Apple Silicon and Intel.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="$ROOT/dist"
UI_DIST="$ROOT/backend/internal/ui/dist"
VERSION="${VERSION:-$(git -C "$ROOT" describe --tags --always --dirty 2>/dev/null || echo dev)}"

echo "==> Frontend (production)"
cd "$ROOT/frontend"
if [[ ! -d node_modules ]]; then
  npm ci
fi
npm run build

echo "==> Embed UI into backend"
rm -rf "$UI_DIST"
mkdir -p "$UI_DIST"
# Angular writes flat files into frontend/dist (browser: ".")
cp -R "$ROOT/frontend/dist/." "$UI_DIST/"
# Drop build metadata that is not needed at runtime
rm -f "$UI_DIST/prerendered-routes.json" "$UI_DIST/3rdpartylicenses.txt"

echo "==> Go binaries (darwin arm64 + amd64)"
mkdir -p "$OUT"
cd "$ROOT/backend"
LDFLAGS="-s -w -X main.version=${VERSION}"
GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/Instaboard-arm64" ./cmd/api
GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT/Instaboard-amd64" ./cmd/api

if command -v lipo >/dev/null 2>&1; then
  lipo -create -output "$OUT/Instaboard" "$OUT/Instaboard-arm64" "$OUT/Instaboard-amd64"
  rm -f "$OUT/Instaboard-arm64" "$OUT/Instaboard-amd64"
else
  # Fallback: ship the host arch only
  ARCH="$(uname -m)"
  if [[ "$ARCH" == "arm64" ]]; then
    mv "$OUT/Instaboard-arm64" "$OUT/Instaboard"
    rm -f "$OUT/Instaboard-amd64"
  else
    mv "$OUT/Instaboard-amd64" "$OUT/Instaboard"
    rm -f "$OUT/Instaboard-arm64"
  fi
fi
chmod +x "$OUT/Instaboard"
# Ad-hoc sign so Apple Silicon will run the binary; Update-and-restart also re-signs the runtime copy.
if command -v codesign >/dev/null 2>&1; then
  codesign --force --sign - --timestamp=none "$OUT/Instaboard" 2>/dev/null || true
fi

echo "==> Zip"
cat > "$OUT/README.txt" <<EOF
Instaboard ${VERSION}

1. Double-click Instaboard (or run it from Terminal: ./Instaboard).
2. Your browser opens to http://localhost:4200/home — keep the Instaboard window open while you use it.
3. Use Restart / Quit beside the Instaboard name in the footer, or press Ctrl+C in that window.

First launch on macOS: if Gatekeeper blocks it, right-click Instaboard → Open → Open.
You need gallery-dl, yt-dlp, and ffmpeg on your PATH (brew install gallery-dl yt-dlp ffmpeg).
For Instagram downloads, stay logged in to Instagram in Chrome.
EOF

ZIP="$OUT/Instaboard-mac.zip"
rm -f "$ZIP"
(
  cd "$OUT"
  zip -q -9 "Instaboard-mac.zip" Instaboard README.txt
)

# Restore embed placeholder so the working tree stays free of hashed frontend assets.
rm -rf "$UI_DIST"
mkdir -p "$UI_DIST"
cat > "$UI_DIST/index.html" <<'HTML'
<!DOCTYPE html><html><head><meta charset="utf-8"><title>Instaboard</title></head>
<body><p>UI not embedded. Run scripts/build-mac.sh or copy frontend/dist here.</p></body></html>
HTML

echo ""
echo "Built: $ZIP"
ls -lh "$ZIP" "$OUT/Instaboard"
echo "Run:   $OUT/Instaboard"
