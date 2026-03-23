package config

import (
	"os"
	"strconv"
)

// Config holds all server configuration, loaded from environment variables.
type Config struct {
	Port        int
	DBPath      string
	PythonBin   string
	PythonDir   string
	MaxWorkers  int
	JobTTLHours int
	QueueSize   int

	R2Endpoint        string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2BucketName      string

	// DatabaseURL, when set, switches persistence to PostgreSQL.
	// Falls back to SQLite (DBPath) when empty.
	DatabaseURL string

	// Auth / account management
	MaxActiveAccounts int
	AccountTTLHours   int
	SessionTTLHours   int

	// AllowedTTSProviders is the comma-separated list of enabled TTS engines.
	AllowedTTSProviders string

	// Per-user lifetime quotas (bytes).
	UserUploadLimitBytes   int64
	UserDownloadLimitBytes int64

	// Telegram bot configuration. Bot is disabled when TelegramBotToken is empty.
	TelegramBotToken string
}

// Load reads configuration from environment variables with sensible defaults.
func Load() Config {
	return Config{
		Port:        envInt("PORT", 8080),
		DBPath:      envStr("DB_PATH", "./data/jobs.db"),
		PythonBin:   envStr("PYTHON_BIN", "python3"),
		PythonDir:   envStr("PYTHON_DIR", "/opt/tc"),
		MaxWorkers:  envInt("MAX_WORKERS", 2),
		JobTTLHours: envInt("JOB_TTL_HOURS", 24),
		QueueSize:   envInt("QUEUE_SIZE", 20),

		R2Endpoint:        envStr("R2_ENDPOINT", ""),
		R2AccessKeyID:     envStr("R2_ACCESS_KEY_ID", ""),
		R2SecretAccessKey: envStr("R2_SECRET_ACCESS_KEY", ""),
		R2BucketName:      envStr("R2_BUCKET_NAME", ""),

		DatabaseURL: envStr("DATABASE_URL", ""),

		MaxActiveAccounts:      envInt("MAX_ACTIVE_ACCOUNTS", 100),
		AccountTTLHours:        envInt("ACCOUNT_TTL_HOURS", 24),
		SessionTTLHours:        envInt("SESSION_TTL_HOURS", 24),
		AllowedTTSProviders:    envStr("ALLOWED_TTS_PROVIDERS", "edge"),
		UserUploadLimitBytes:   envInt64("USER_UPLOAD_LIMIT_BYTES", 1<<30),   // 1 GB
		UserDownloadLimitBytes: envInt64("USER_DOWNLOAD_LIMIT_BYTES", 3<<30), // 3 GB

		TelegramBotToken: envStr("TELEGRAM_BOT_TOKEN", ""),
	}
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return fallback
}
