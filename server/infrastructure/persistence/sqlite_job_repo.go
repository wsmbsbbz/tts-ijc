package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"

	_ "modernc.org/sqlite"
)

// SQLiteJobRepo implements domain.JobRepository using SQLite.
type SQLiteJobRepo struct {
	db *sql.DB
}

// NewSQLiteJobRepo opens the SQLite database and creates the jobs table if needed.
func NewSQLiteJobRepo(dbPath string) (*SQLiteJobRepo, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// Enable WAL for better concurrent read performance.
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if err := createTable(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migrateTable(db); err != nil {
		db.Close()
		return nil, err
	}

	return &SQLiteJobRepo{db: db}, nil
}

func createTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS jobs (
			id           TEXT PRIMARY KEY,
			status       TEXT NOT NULL DEFAULT 'queued',
			progress     TEXT NOT NULL DEFAULT '',
			audio_key    TEXT NOT NULL,
			vtt_key      TEXT NOT NULL,
			output_key   TEXT NOT NULL DEFAULT '',
			audio_name   TEXT NOT NULL DEFAULT '',
			vtt_name     TEXT NOT NULL DEFAULT '',
			tts_provider TEXT NOT NULL DEFAULT 'edge',
			tts_volume   REAL NOT NULL DEFAULT 0.08,
			no_speedup   INTEGER NOT NULL DEFAULT 0,
			concurrency  INTEGER NOT NULL DEFAULT 5,
			created_at   TEXT NOT NULL,
			completed_at TEXT,
			error        TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	return nil
}

// migrateTable adds columns introduced after the initial schema (idempotent).
func migrateTable(db *sql.DB) error {
	for _, stmt := range []string{
		"ALTER TABLE jobs ADD COLUMN audio_name TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE jobs ADD COLUMN vtt_name   TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE jobs ADD COLUMN user_id    TEXT NOT NULL DEFAULT ''",
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// Close releases the database connection.
func (r *SQLiteJobRepo) Close() error {
	return r.db.Close()
}

func (r *SQLiteJobRepo) Save(ctx context.Context, job domain.Job) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO jobs (id, user_id, status, progress, audio_key, vtt_key, output_key,
		                  audio_name, vtt_name,
		                  tts_provider, tts_volume, no_speedup, concurrency,
		                  created_at, completed_at, error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		job.ID, job.UserID, string(job.Status), job.Progress,
		job.AudioKey, job.VTTKey, job.OutputKey,
		job.AudioName, job.VTTName,
		job.Config.TTSProvider, job.Config.TTSVolume,
		boolToInt(job.Config.NoSpeedup), job.Config.Concurrency,
		job.CreatedAt.Format(time.RFC3339),
		timePtr(job.CompletedAt),
		job.Error,
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

func (r *SQLiteJobRepo) FindByID(ctx context.Context, id string) (domain.Job, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, status, progress, audio_key, vtt_key, output_key,
		       audio_name, vtt_name,
		       tts_provider, tts_volume, no_speedup, concurrency,
		       created_at, completed_at, error
		FROM jobs WHERE id = ?
	`, id)
	return scanJob(row)
}

func (r *SQLiteJobRepo) ListRecent(ctx context.Context, userID string, limit int) ([]domain.Job, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, status, progress, audio_key, vtt_key, output_key,
		       audio_name, vtt_name,
		       tts_provider, tts_volume, no_speedup, concurrency,
		       created_at, completed_at, error
		FROM jobs WHERE user_id = ? ORDER BY created_at DESC LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *SQLiteJobRepo) UpdateStatus(ctx context.Context, id string, status domain.Status, progress string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status = ?, progress = ? WHERE id = ?
	`, string(status), progress, id)
	return checkAffected(res, err, id)
}

func (r *SQLiteJobRepo) SetCompleted(ctx context.Context, id, outputKey string) error {
	now := time.Now().Format(time.RFC3339)
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status = ?, output_key = ?, completed_at = ? WHERE id = ?
	`, string(domain.StatusCompleted), outputKey, now, id)
	return checkAffected(res, err, id)
}

func (r *SQLiteJobRepo) SetFailed(ctx context.Context, id, errMsg string) error {
	now := time.Now().Format(time.RFC3339)
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status = ?, error = ?, completed_at = ? WHERE id = ?
	`, string(domain.StatusFailed), errMsg, now, id)
	return checkAffected(res, err, id)
}

func (r *SQLiteJobRepo) DeleteExpired(ctx context.Context, ttl time.Duration) ([]domain.Job, error) {
	cutoff := time.Now().Add(-ttl).Format(time.RFC3339)

	// First read the expired jobs so we can return them for R2 cleanup.
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, status, progress, audio_key, vtt_key, output_key,
		       audio_name, vtt_name,
		       tts_provider, tts_volume, no_speedup, concurrency,
		       created_at, completed_at, error
		FROM jobs WHERE created_at < ? AND status IN (?, ?)
	`, cutoff, string(domain.StatusCompleted), string(domain.StatusFailed))
	if err != nil {
		return nil, fmt.Errorf("query expired: %w", err)
	}
	defer rows.Close()

	var expired []domain.Job
	for rows.Next() {
		job, err := scanJobRows(rows)
		if err != nil {
			return nil, err
		}
		expired = append(expired, job)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(expired) > 0 {
		_, err = r.db.ExecContext(ctx, `
			DELETE FROM jobs WHERE created_at < ? AND status IN (?, ?)
		`, cutoff, string(domain.StatusCompleted), string(domain.StatusFailed))
		if err != nil {
			return nil, fmt.Errorf("delete expired: %w", err)
		}
	}

	return expired, nil
}

// --- helpers ---

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(s scanner) (domain.Job, error) {
	var (
		j           domain.Job
		status      string
		noSpeedup   int
		createdAt   string
		completedAt sql.NullString
		errMsg      sql.NullString
	)

	err := s.Scan(
		&j.ID, &j.UserID, &status, &j.Progress,
		&j.AudioKey, &j.VTTKey, &j.OutputKey,
		&j.AudioName, &j.VTTName,
		&j.Config.TTSProvider, &j.Config.TTSVolume,
		&noSpeedup, &j.Config.Concurrency,
		&createdAt, &completedAt, &errMsg,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Job{}, domain.ErrJobNotFound
		}
		return domain.Job{}, fmt.Errorf("scan job: %w", err)
	}

	j.Status = domain.Status(status)
	j.Config.NoSpeedup = noSpeedup != 0
	j.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	if completedAt.Valid {
		t, _ := time.Parse(time.RFC3339, completedAt.String)
		j.CompletedAt = &t
	}
	if errMsg.Valid {
		j.Error = &errMsg.String
	}

	return j, nil
}

func scanJobRows(rows *sql.Rows) (domain.Job, error) {
	return scanJob(rows)
}

func checkAffected(res sql.Result, err error, id string) error {
	if err != nil {
		return fmt.Errorf("update job %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return domain.ErrJobNotFound
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func timePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}
