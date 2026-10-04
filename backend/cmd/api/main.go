// Command api is the Falconia backend.
//
// The process listens on TWO ports, and that is not a deployment detail:
//
//	:8080  the application — sessions, API, MCP
//	:8081  the content     — artifact bytes, and nothing else
//
// Two different hostnames sit in front of them, hence two different origins,
// hence the code inside an artifact cannot read the session of whoever is
// looking at it even if everything else went wrong. The configuration refuses
// to start when the two origins coincide: an install that believes it is
// protected and is not would be worse than one that does not start.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/clackes/falconia/backend/internal/agentbridge"
	"github.com/clackes/falconia/backend/internal/blob"
	"github.com/clackes/falconia/backend/internal/config"
	"github.com/clackes/falconia/backend/internal/content"
	"github.com/clackes/falconia/backend/internal/handlers"
	"github.com/clackes/falconia/backend/internal/storage"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("configuration", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := storage.NewPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("database connection", "err", err)
		os.Exit(1)
	}
	defer store.Close()

	// Migrations run at startup inside an advisory lock: two replicas starting
	// together do not fight over them, and nobody has to remember to run psql
	// by hand after an upgrade.
	if err := store.Migrate(ctx, logger); err != nil {
		logger.Error("migration", "err", err)
		os.Exit(1)
	}
	if err := store.PurgeExpiredDeviceCodes(ctx); err != nil {
		logger.Warn("device code cleanup", "err", err)
	}

	blobs, err := openBlobStore(ctx, cfg)
	if err != nil {
		logger.Error("object storage", "err", err)
		os.Exit(1)
	}

	appSrv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: handlers.NewRouter(handlers.Deps{
			Store:             store,
			Blobs:             blobs,
			Agents:            agentbridge.NewHub(),
			AppBaseURL:        cfg.AppBaseURL,
			ContentBaseURL:    cfg.ContentBaseURL,
			APIBaseURL:        cfg.APIBaseURL,
			JWTSecret:         cfg.JWTSecret,
			ContentSecret:     cfg.ContentSecret,
			ContentGrantTTL:   cfg.ContentGrantTTL,
			AllowRegistration: cfg.AllowRegistration,
			BlobBackend:       cfg.BlobBackend,
			MaxUploadBytes:    cfg.MaxUploadBytes,
			MaxBundleFiles:    cfg.MaxBundleFiles,
			Logger:            logger,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		// No global WriteTimeout: /api/chat streams SSE for minutes, and a
		// write timeout would cut it off mid-sentence.
		IdleTimeout: 120 * time.Second,
	}

	contentSrv := &http.Server{
		Addr: cfg.ContentListenAddr,
		Handler: (&content.Server{
			Store:     store,
			Blobs:     blobs,
			Secret:    cfg.ContentSecret,
			AppOrigin: cfg.AppBaseURL,
			Logger:    logger,
		}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      120 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go serve(appSrv, "app", logger)
	go serve(contentSrv, "content", logger)
	logger.Info("falconia listening", "version", handlers.Version,
		"app", cfg.ListenAddr, "content", cfg.ContentListenAddr,
		"app_url", cfg.AppBaseURL, "content_url", cfg.ContentBaseURL,
		"blobs", cfg.BlobBackend)

	<-ctx.Done()
	logger.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	// Both servers stop in parallel: in series, an SSE connection still open on
	// the application would hold up the other one too.
	done := make(chan struct{}, 2)
	for _, s := range []*http.Server{appSrv, contentSrv} {
		go func(s *http.Server) {
			if err := s.Shutdown(shutdownCtx); err != nil {
				logger.Warn("shutdown", "addr", s.Addr, "err", err)
			}
			done <- struct{}{}
		}(s)
	}
	<-done
	<-done
	logger.Info("stopped")
}

func serve(s *http.Server, name string, logger *slog.Logger) {
	if err := s.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server", "name", name, "err", err)
		os.Exit(1)
	}
}

func openBlobStore(ctx context.Context, cfg *config.Config) (blob.Store, error) {
	if cfg.BlobBackend == "s3" {
		return blob.NewS3(ctx, blob.S3Config{
			Endpoint:  cfg.S3Endpoint,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Bucket:    cfg.S3Bucket,
			Region:    cfg.S3Region,
			UseSSL:    cfg.S3UseSSL,
		})
	}
	return blob.NewFS(cfg.BlobRoot)
}
