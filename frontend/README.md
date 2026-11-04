# Instaboard frontend

Angular 21 app for Instaboard (Home, folders, downloads). It talks to the local Go backend
(`instaboard-backend`), which must be running.

## Run

```sh
npm install
npm start          # http://localhost:5281, API at http://localhost:8788
```

`npm run build` produces `dist/`, which the backend serves in the packaged app (API calls go to the
same origin in production builds).

## Notes

- Styling is Tailwind via the CDN, configured inline in `index.html` (colours, Shopify-style type scale,
  shadows).
- There is no sign-in: on load the app asks the local backend for a session (`POST /auth/local`).
