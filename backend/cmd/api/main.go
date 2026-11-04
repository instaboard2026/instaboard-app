package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/httprate"
	"github.com/joho/godotenv"
	"instaboard/internal/config"
	"instaboard/internal/database"
	"instaboard/internal/handler"
	corspkg "instaboard/internal/middleware"
	"instaboard/internal/repository"
	"instaboard/internal/service"
	"instaboard/migrations"
)

func main() {
	// Optional overrides (PORT, DB_PATH, DOWNLOAD_DIR, ...); every setting has a default.
	_ = godotenv.Load(".env")
	cfg := config.Load()
	if err := cfg.EnsureJWTSecret(); err != nil {
		slog.Error("session key setup failed", "err", err)
		os.Exit(1)
	}

	db, err := database.NewSQLite(cfg.DBPath)
	if err != nil {
		slog.Error("database open failed", "path", cfg.DBPath, "err", err)
		os.Exit(1)
	}
	applied, err := database.Migrate(context.Background(), db, migrations.FS)
	if err != nil {
		slog.Error("database migration failed", "err", err)
		os.Exit(1)
	}
	slog.Info("database ready", "path", cfg.DBPath, "migrations_applied", applied)
	defer db.Close()

	userRepo := repository.NewUserRepo(db)
	authSvc := service.NewAuthService(userRepo)
	authHandler := handler.NewAuthHandler(cfg, authSvc)
	profileHandler := handler.NewProfileHandler(userRepo)
	saveRepo := repository.NewSaveRepo(db)
	downloader := service.NewDownloader(cfg.GalleryDLPath, cfg.CookiesFromBrowser, cfg.YTDLModule)
	settingsRepo := repository.NewSettingsRepo(db)
	mediaSigner := service.NewMediaSigner(cfg.JWTSecret, cfg.BackendPublicURL, 24*time.Hour)
	dumpHandler := handler.NewDumpHandler(saveRepo, settingsRepo, downloader, mediaSigner, cfg.DownloadDir, cfg.CookiesFromBrowser)
	folderHandler := handler.NewFolderHandler(repository.NewFolderRepo(db), saveRepo, dumpHandler)

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(corspkg.CORS(cfg.CORSHost))

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	r.Get("/media/{uid}/{id}/{file}", dumpHandler.Media)
	r.With(httprate.LimitByIP(60, time.Minute)).Post("/auth/local", authHandler.Local)

	r.Route("/api", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(corspkg.RequireJWT(cfg))
			r.Get("/profile", profileHandler.GetProfile)
			r.Get("/dump", dumpHandler.GetDump)
			r.Get("/stats", dumpHandler.Stats)
			r.Post("/dump", dumpHandler.AddToDump)
			r.Post("/dump/{id}/download", dumpHandler.Download)
			r.Get("/folders", folderHandler.List)
			r.Post("/folders", folderHandler.Create)
			r.Put("/folders/order", folderHandler.Reorder)
			r.Patch("/folders/{id}", folderHandler.Rename)
			r.Delete("/folders/{id}", folderHandler.Delete)
			r.Get("/folders/{id}/saves", folderHandler.ListSaves)
			r.Post("/folders/{id}/saves", folderHandler.AddSave)
			r.Patch("/saves/{id}", folderHandler.MoveSave)
			r.Get("/downloads/info", dumpHandler.DownloadsInfo)
			r.Post("/downloads/folder", dumpHandler.ChooseDownloadFolder)
			r.Delete("/downloads/folder", dumpHandler.ResetDownloadFolder)
		})
	})

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	slog.Info("server starting", "addr", addr)
	if err := http.ListenAndServe(addr, r); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}
