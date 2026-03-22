package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/wsmbsbbz/tts-ijc/server/application"
	"github.com/wsmbsbbz/tts-ijc/server/domain"
	"github.com/wsmbsbbz/tts-ijc/server/infrastructure/config"
	"github.com/wsmbsbbz/tts-ijc/server/infrastructure/persistence"
	"github.com/wsmbsbbz/tts-ijc/server/infrastructure/storage"
	"github.com/wsmbsbbz/tts-ijc/server/infrastructure/translator"
	httpintf "github.com/wsmbsbbz/tts-ijc/server/interfaces/http"
)

func main() {
	cfg := config.Load()

	// --- Persistence ---

	repo := initRepo(cfg)

	// --- Infrastructure ---

	r2 := storage.NewR2Storage(
		cfg.R2Endpoint,
		cfg.R2AccessKeyID,
		cfg.R2SecretAccessKey,
		cfg.R2BucketName,
	)

	trans := translator.NewPythonTranslator(cfg.PythonBin, cfg.PythonDir)

	// --- Application ---

	idFunc := func() string { return uuid.New().String() }
	queue := make(chan string, cfg.QueueSize)

	jobSvc := application.NewJobService(repo, queue, idFunc)
	uploadSvc := application.NewUploadService(r2, idFunc)
	workerSvc := application.NewWorkerService(repo, r2, trans, queue)

	// --- Start workers ---

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerSvc.Start(ctx, cfg.MaxWorkers)
	workerSvc.StartCleanupLoop(ctx, 1*time.Hour, time.Duration(cfg.JobTTLHours)*time.Hour)

	log.Printf("started %d workers, queue capacity %d", cfg.MaxWorkers, cfg.QueueSize)

	// --- HTTP ---

	jobHandler := httpintf.NewJobHandler(jobSvc, r2)
	uploadHandler := httpintf.NewUploadHandler(uploadSvc)

	router := httpintf.NewRouter(jobHandler, uploadHandler,
		httpintf.BasicAuth(cfg.AuthUser, cfg.AuthPass),
	)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Port),
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// --- Graceful shutdown ---

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("shutting down...")
		cancel()

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		srv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on :%d", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// repoCloser wraps a JobRepository with a Close method.
type repoCloser interface {
	domain.JobRepository
	Close() error
}

// initRepo selects the persistence backend based on config.
// When DATABASE_URL is set, uses PostgreSQL; otherwise falls back to SQLite.
func initRepo(cfg config.Config) repoCloser {
	if cfg.DatabaseURL != "" {
		repo, err := persistence.NewPostgresJobRepo(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("init postgres: %v", err)
		}
		log.Println("persistence: postgres")
		return repo
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		log.Fatalf("create db directory: %v", err)
	}
	repo, err := persistence.NewSQLiteJobRepo(cfg.DBPath)
	if err != nil {
		log.Fatalf("init sqlite: %v", err)
	}
	log.Printf("persistence: sqlite (%s)", cfg.DBPath)
	return repo
}
