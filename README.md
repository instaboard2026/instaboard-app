# Instaboard

A local-first board for the Instagram reels and carousels, YouTube videos and Pinterest pins you want to
keep. Paste a link, download it to your own computer, and sort it into folders. Everything runs and is
stored on your machine; there is no account or cloud.

## Download (macOS)

1. Grab the latest **Instaboard-mac.zip** from [Releases](https://github.com/instaboard2026/instaboard/releases).
2. Unzip, then double-click **Instaboard** (or run `./Instaboard` in Terminal).
3. Your browser opens to `http://127.0.0.1:8788`. Keep that window open while you use the app.

First launch: if macOS blocks the unsigned binary, right-click → Open → Open.

Also install download tools once:

```sh
brew install gallery-dl yt-dlp ffmpeg
```

For Instagram, stay logged in to Instagram in Chrome (macOS may ask once for Keychain access).

## Layout

| Folder | What |
|--------|------|
| [`backend/`](backend) | Go server: API, SQLite, downloads, and (in release builds) the embedded UI |
| [`frontend/`](frontend) | Angular app |
| [`scripts/build-mac.sh`](scripts/build-mac.sh) | Builds a universal Mac zip |

## Develop (two processes)

```sh
brew install go node gallery-dl yt-dlp ffmpeg

# terminal 1
cd backend && go run ./cmd/api          # http://127.0.0.1:8788

# terminal 2
cd frontend && npm install && npm start # http://localhost:5281
```

Open http://localhost:5281.

## Package a Mac zip

```sh
./scripts/build-mac.sh
# → dist/Instaboard-mac.zip
```
