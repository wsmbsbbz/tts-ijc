package main

import (
	"context"
	"database/sql"
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
	"github.com/wsmbsbbz/tts-ijc/server/interfaces/telegram"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func main() {
	cfg := config.Load()

	// --- Persistence ---

	jobRepo, userRepo, sessionRepo, bindingRepo, closeDB := initRepos(cfg)
	defer closeDB()

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

	accountTTL := time.Duration(cfg.AccountTTLHours) * time.Hour
	sessionTTL := time.Duration(cfg.SessionTTLHours) * time.Hour

	jobSvc := application.NewJobService(jobRepo, queue, idFunc)
	uploadSvc := application.NewUploadService(r2, userRepo, idFunc, cfg.UserUploadLimitBytes)
	workerSvc := application.NewWorkerService(jobRepo, r2, trans, queue)
	authSvc := application.NewAuthService(userRepo, sessionRepo, idFunc, accountTTL, sessionTTL, cfg.MaxActiveAccounts)

	// --- Start workers ---

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	workerSvc.Start(ctx, cfg.MaxWorkers)
	workerSvc.StartCleanupLoop(ctx, 1*time.Hour, time.Duration(cfg.JobTTLHours)*time.Hour)

	// Periodically expire accounts and their sessions.
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := authSvc.ExpireAccounts(ctx); err != nil {
					log.Printf("expire accounts: %v", err)
				}
			}
		}
	}()

	log.Printf("started %d workers, queue capacity %d", cfg.MaxWorkers, cfg.QueueSize)

	// --- HTTP ---

	jobHandler := httpintf.NewJobHandler(jobSvc, r2, userRepo, cfg.AllowedTTSProviders, cfg.UserDownloadLimitBytes)
	uploadHandler := httpintf.NewUploadHandler(uploadSvc)
	authHandler := httpintf.NewAuthHandler(authSvc, cfg.UserUploadLimitBytes, cfg.UserDownloadLimitBytes)
	sessionAuth := httpintf.SessionAuth(authSvc)

	router := httpintf.NewRouter(jobHandler, uploadHandler, authHandler, sessionAuth)

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

	// --- Telegram bot (optional) ---

	if cfg.TelegramBotToken != "" {
		botSrv := telegram.NewBotServer(telegram.BotConfig{
			Token:            cfg.TelegramBotToken,
			JobSvc:           jobSvc,
			AuthSvc:          authSvc,
			Storage:          r2,
			UserRepo:         userRepo,
			BindingRepo:      bindingRepo,
			IDFunc:           idFunc,
			AllowedProviders: cfg.AllowedTTSProviders,
			UploadLimit:      cfg.UserUploadLimitBytes,
			DownloadLimit:    cfg.UserDownloadLimitBytes,
		})
		go botSrv.Start(ctx)
		log.Println("telegram bot: enabled")
	}

	log.Printf("listening on :%d", cfg.Port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

// initRepos selects the persistence backend based on config.
// Returns job, user, session, and telegram binding repos plus a close function.
func initRepos(cfg config.Config) (domain.JobRepository, domain.UserRepository, domain.SessionRepository, domain.TelegramBindingRepository, func()) {
	if cfg.DatabaseURL != "" {
		db, err := sql.Open("pgx", cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("open postgres: %v", err)
		}
		if err := db.Ping(); err != nil {
			log.Fatalf("ping postgres: %v", err)
		}

		jobRepo, err := persistence.NewPostgresJobRepo(cfg.DatabaseURL)
		if err != nil {
			log.Fatalf("init postgres job repo: %v", err)
		}

		userRepo, sessionRepo, err := persistence.NewPostgresUserRepos(db)
		if err != nil {
			log.Fatalf("init postgres user repos: %v", err)
		}

		bindingRepo, err := persistence.NewPostgresTelegramBindingRepo(db)
		if err != nil {
			log.Fatalf("init postgres telegram binding repo: %v", err)
		}

		log.Println("persistence: postgres")
		return jobRepo, userRepo, sessionRepo, bindingRepo, func() {
			jobRepo.Close()
			db.Close()
		}
	}

	if err := os.MkdirAll(filepath.Dir(cfg.DBPath), 0o755); err != nil {
		log.Fatalf("create db directory: %v", err)
	}

	jobRepo, err := persistence.NewSQLiteJobRepo(cfg.DBPath)
	if err != nil {
		log.Fatalf("init sqlite job repo: %v", err)
	}

	// Share the same SQLite file for user/session/binding tables.
	db, err := openSQLiteDB(cfg.DBPath)
	if err != nil {
		log.Fatalf("open sqlite for user repos: %v", err)
	}

	userRepo, sessionRepo, err := persistence.NewSQLiteUserRepos(db)
	if err != nil {
		log.Fatalf("init sqlite user repos: %v", err)
	}

	bindingRepo, err := persistence.NewSQLiteTelegramBindingRepo(db)
	if err != nil {
		log.Fatalf("init sqlite telegram binding repo: %v", err)
	}

	log.Printf("persistence: sqlite (%s)", cfg.DBPath)
	return jobRepo, userRepo, sessionRepo, bindingRepo, func() {
		jobRepo.Close()
		db.Close()
	}
}

func openSQLiteDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	return db, nil
}
