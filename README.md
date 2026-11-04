# Instaboard

A local-first board for the Instagram reels and carousels, YouTube videos and Pinterest pins you want to
keep. Paste a link, download it to your own computer, and sort it into folders. Everything runs and is
stored on your machine; there is no account or cloud.

## Layout

| Folder | What |
|--------|------|
| [`backend/`](backend) | Go server: API, SQLite database, downloads via gallery-dl + yt-dlp + ffmpeg |
| [`frontend/`](frontend) | Angular app |

## Run it (macOS)

```sh
brew install go node gallery-dl yt-dlp ffmpeg

# terminal 1
cd backend && go run ./cmd/api          # http://127.0.0.1:8788

# terminal 2
cd frontend && npm install && npm start # http://localhost:5281
```

Open http://localhost:5281. You're signed in automatically as your Mac user. To download from Instagram,
be logged in to Instagram in Chrome (Instaboard uses that session; macOS asks once for Keychain access).
