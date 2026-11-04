# Instaboard backend

Local Go server for Instaboard: saves Instagram / Pinterest / YouTube links, downloads them with
gallery-dl + yt-dlp + ffmpeg, and organises them into folders. Data lives in SQLite on your machine.

## Run

```sh
brew install gallery-dl yt-dlp ffmpeg
go run ./cmd/api
```

The server listens on `127.0.0.1:8788`. The database (`~/.instaboard/instaboard.db`) and its schema are
created on first run, and you're signed in automatically as your computer account.

## Settings

Everything has a default. To override, set environment variables or put them in a `.env` file:

| Variable | Default |
|----------|---------|
| `PORT` | `8788` |
| `HOST` | `127.0.0.1` |
| `CORS_ORIGIN` | `http://localhost:5281` (the frontend dev server) |
| `DB_PATH` | `~/.instaboard/instaboard.db` |
| `DOWNLOAD_DIR` | `~/Movies/Instaboard` (can also be changed in the app) |
| `COOKIES_FROM_BROWSER` | `chrome` (Instagram session to use; `none` to stay logged out) |
| `GALLERY_DL_PATH` | `gallery-dl` |
| `JWT_SECRET` | generated into `~/.instaboard/session.key` |

## Layout

- `cmd/api` – entry point and routes
- `internal/` – config, database (SQLite + migrations runner), handlers, repositories, services
- `migrations/` – schema, applied automatically in name order
