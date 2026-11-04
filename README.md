# Instaboard

A local-first board for the Instagram reels and carousels, YouTube videos and Pinterest pins you want to
keep. Paste a link, download it to your own computer, and sort it into folders. Everything runs and is
stored on your machine; there is no account or cloud.

## Download (macOS)

1. Grab the latest **Instaboard-mac.zip** from [Releases](https://github.com/instaboard2026/instaboard-app/releases).
2. Unzip, then double-click **Instaboard** (or run `./Instaboard` in Terminal).
3. Your browser opens to `http://localhost:4200/home`. Keep that window open while you use the app.

First launch: if macOS blocks the unsigned binary, right-click → Open → Open.

**Download tools:** if gallery-dl / yt-dlp / ffmpeg aren’t on your Mac yet, open Instaboard and click **Install tools** in the footer (or in Downloads). No restart needed. Homebrew is used when available.

For Instagram, stay logged in to Instagram in Chrome (macOS may ask once for Keychain access).

## Layout

| Folder | What |
|--------|------|
| [`backend/`](backend) | Go server: API, SQLite, downloads, and (in release builds) the embedded UI |
| [`frontend/`](frontend) | Angular app |
| [`scripts/build-mac.sh`](scripts/build-mac.sh) | Builds a universal Mac zip |

## Develop

```sh
brew install go node gallery-dl yt-dlp ffmpeg
```

Local ports (different from the packaged app on **4200**):

| Process | URL |
|---------|-----|
| Backend | `http://localhost:8788` |
| Frontend | `http://localhost:5200` |

One-liners (same pattern as Get Funding): rebuild/restart in the background, logs under `../.dev/logs/`.

```sh
# full stack
./scripts/dev-instaboard-rebuild-restart.sh

# backend only (Go, port 8788)
./scripts/dev-instaboard-rebuild-backend.sh

# frontend only (Angular, port 5200) — use QUICK=1 to skip npm wipe
./scripts/dev-instaboard-rebuild-frontend.sh
QUICK=1 ./scripts/dev-instaboard-rebuild-frontend.sh
```

Or two terminals:

```sh
cd backend && go run ./cmd/api          # http://localhost:8788
cd frontend && npm install && npm start # http://localhost:5200
```

Open http://localhost:5200.

## Package a Mac zip

```sh
./scripts/build-mac.sh
# → dist/Instaboard-mac.zip
```

## Ship an update

The footer **Updates** chip shows when you’re behind; open it and **Download update** saves `Instaboard-mac.zip` to `~/Downloads`.

```sh
git tag v0.1.1
VERSION=v0.1.1 ./scripts/build-mac.sh
gh release create v0.1.1 dist/Instaboard-mac.zip --repo instaboard2026/instaboard-app \
  --title "Instaboard v0.1.1" --notes "What changed…"
```

The release asset **must** be named `Instaboard-mac.zip` so the updater finds it. Set `UPDATE_CHECK=0` to disable checks.
