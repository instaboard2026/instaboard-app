package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
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
	"instaboard/internal/ui"
	"instaboard/migrations"
	"instaboard/pkg/utils"
)

// Set at link time by scripts/build-mac.sh (-X main.version=...).
var version = "dev"

func main() {
	// If Update-and-restart installed a newer copy under ~/.instaboard/runtime/, switch to it.
	// The double-clicked launcher the user Allowed is left untouched (no Gatekeeper re-prompt).
	service.MaybeExecRuntime(version)

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
	slog.Info("database ready", "path", cfg.DBPath, "migrations_applied", applied, "version", version)
	defer db.Close()

	embeddedUI := ui.HasUI()
	if embeddedUI {
		// Packaged app: UI + API on :4200 so users land on http://localhost:4200/home.
		// Dev (no embed) keeps PORT default 8788. Explicit PORT= still wins.
		if os.Getenv("PORT") == "" {
			cfg.Port = 4200
		}
		cfg.BackendPublicURL = fmt.Sprintf("http://localhost:%d", cfg.Port)
		cfg.CORSHost = cfg.BackendPublicURL
	}

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
	calendarHandler := handler.NewCalendarHandler(repository.NewCalendarRepo(db))

	var srv *http.Server
	var restartAfter atomic.Bool
	triggerRestart := func() {
		restartAfter.Store(true)
		go gracefulShutdown(srv)
	}
	updateHandler := handler.NewUpdateHandler(service.NewUpdater(version), triggerRestart)

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
			r.Get("/calendar", calendarHandler.Get)
			r.Put("/calendar/saves/{id}", calendarHandler.Schedule)
			r.Delete("/calendar/saves/{id}", calendarHandler.Unschedule)
			r.Put("/calendar/notes/{date}", calendarHandler.SetNote)
			r.Get("/dump", dumpHandler.GetDump)
			r.Get("/stats", dumpHandler.Stats)
			r.Post("/dump", dumpHandler.AddToDump)
			r.Post("/dump/upload", dumpHandler.UploadToDump)
			r.Put("/dump/order", dumpHandler.Reorder)
			r.Post("/dump/{id}/download", dumpHandler.Download)
			r.Post("/dump/{id}/reveal", dumpHandler.RevealInFinder)
			r.Get("/folders", folderHandler.List)
			r.Post("/folders", folderHandler.Create)
			r.Put("/folders/order", folderHandler.Reorder)
			r.Patch("/folders/{id}", folderHandler.Rename)
			r.Delete("/folders/{id}", folderHandler.Delete)
			r.Get("/folders/{id}/saves", folderHandler.ListSaves)
			r.Post("/folders/{id}/saves", folderHandler.AddSave)
			r.Post("/folders/{id}/saves/upload", folderHandler.UploadToFolder)
			r.Put("/folders/{id}/saves/order", folderHandler.ReorderSaves)
			r.Patch("/saves/{id}", folderHandler.MoveSave)
			r.Delete("/saves/{id}", folderHandler.DeleteSave)
			r.Post("/saves/{id}/restore", folderHandler.RestoreSave)
			r.Get("/bin", folderHandler.ListBin)
			r.Delete("/bin", folderHandler.ClearBin)
			r.Get("/downloads/info", dumpHandler.DownloadsInfo)
			r.Post("/downloads/folder", dumpHandler.ChooseDownloadFolder)
			r.Delete("/downloads/folder", dumpHandler.ResetDownloadFolder)
			r.Post("/downloads/tools", dumpHandler.InstallDownloadTools)
			r.Delete("/downloads/tools", dumpHandler.UninstallDownloadTools)
			r.Get("/update", updateHandler.Check)
			r.Post("/update/download", updateHandler.Download)
			r.Post("/update/apply", updateHandler.Apply)
			// Quit / Restart from the UI (Quit = Ctrl+C; Restart relaunches this binary after shutdown).
			r.Post("/quit", func(w http.ResponseWriter, req *http.Request) {
				utils.WriteSuccess(w, map[string]string{"ok": "true"})
				go gracefulShutdown(srv)
			})
			r.Post("/restart", func(w http.ResponseWriter, req *http.Request) {
				utils.WriteSuccess(w, map[string]string{"ok": "true"})
				triggerRestart()
			})
		})
	})

	if embeddedUI {
		r.NotFound(ui.Handler().ServeHTTP)
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	// Prefer localhost in the browser URL (friendlier than 127.0.0.1); still bind to loopback Host.
	publicURL := cfg.BackendPublicURL
	if publicURL == "" {
		publicURL = fmt.Sprintf("http://localhost:%d", cfg.Port)
	}
	srv = &http.Server{Addr: addr, Handler: r}
	openURL := publicURL
	if embeddedUI {
		openURL = strings.TrimRight(publicURL, "/") + "/home"
		fmt.Fprintf(os.Stderr, "\n  Instaboard is running at %s\n  Keep this window open. Use Restart/Quit in the app, or press Ctrl+C.\n\n", openURL)
	}
	slog.Info("server starting", "addr", addr, "ui", embeddedUI, "url", openURL)

	if shouldOpenBrowser(embeddedUI) {
		go openBrowser(openURL)
	}

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
	slog.Info("server stopped")

	if restartAfter.Load() {
		if err := relaunchSelf(); err != nil {
			slog.Error("restart failed", "err", err)
			os.Exit(1)
		}
	}
}

func gracefulShutdown(srv *http.Server) {
	time.Sleep(150 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Warn("shutdown", "err", err)
	}
}

// relaunchSelf starts a new Instaboard process (detached) so Restart survives this process exiting.
// Prefer the original launcher path so macOS does not re-prompt Gatekeeper for the runtime binary.
func relaunchSelf() error {
	exe, err := service.RelaunchExecutable()
	if err != nil {
		return err
	}

	env := os.Environ()
	// Prefer opening the browser for the new instance (UI Restart).
	filtered := make([]string, 0, len(env)+1)
	for _, e := range env {
		if strings.HasPrefix(e, "OPEN_BROWSER=") {
			continue
		}
		filtered = append(filtered, e)
	}
	filtered = append(filtered, "OPEN_BROWSER=1")

	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = filtered
	cmd.Dir, _ = os.Getwd()
	if runtime.GOOS != "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	slog.Info("restarted", "pid", cmd.Process.Pid, "exe", exe)
	_ = cmd.Process.Release()
	return nil
}

func shouldOpenBrowser(embeddedUI bool) bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("OPEN_BROWSER")))
	switch v {
	case "0", "false", "no":
		return false
	case "1", "true", "yes":
		return true
	default:
		return embeddedUI
	}
}

func openBrowser(url string) {
	time.Sleep(400 * time.Millisecond)
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		slog.Warn("could not open browser", "err", err, "url", url)
	}
}
