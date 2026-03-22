package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// PostgresJobRepo implements domain.JobRepository using PostgreSQL.
type PostgresJobRepo struct {
	db *sql.DB
}

// NewPostgresJobRepo opens the Postgres database and creates the jobs table if needed.
func NewPostgresJobRepo(dsn string) (*PostgresJobRepo, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}

	if err := createPostgresTable(db); err != nil {
		db.Close()
		return nil, err
	}
	if err := migratePostgresTable(db); err != nil {
		db.Close()
		return nil, err
	}

	return &PostgresJobRepo{db: db}, nil
}

func createPostgresTable(db *sql.DB) error {
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
			tts_volume   DOUBLE PRECISION NOT NULL DEFAULT 0.08,
			no_speedup   BOOLEAN NOT NULL DEFAULT FALSE,
			concurrency  INTEGER NOT NULL DEFAULT 5,
			created_at   TIMESTAMPTZ NOT NULL,
			completed_at TIMESTAMPTZ,
			error        TEXT
		)
	`)
	if err != nil {
		return fmt.Errorf("create table: %w", err)
	}
	return nil
}

// migratePostgresTable adds columns introduced after the initial schema (idempotent).
func migratePostgresTable(db *sql.DB) error {
	for _, stmt := range []string{
		"ALTER TABLE jobs ADD COLUMN IF NOT EXISTS audio_name TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE jobs ADD COLUMN IF NOT EXISTS vtt_name   TEXT NOT NULL DEFAULT ''",
		"ALTER TABLE jobs ADD COLUMN IF NOT EXISTS user_id    TEXT NOT NULL DEFAULT ''",
	} {
		if _, err := db.Exec(stmt); err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// Close releases the database connection.
func (r *PostgresJobRepo) Close() error {
	return r.db.Close()
}

func (r *PostgresJobRepo) Save(ctx context.Context, job domain.Job) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO jobs (id, user_id, status, progress, audio_key, vtt_key, output_key,
		                  audio_name, vtt_name,
		                  tts_provider, tts_volume, no_speedup, concurrency,
		                  created_at, completed_at, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
	`,
		job.ID, job.UserID, string(job.Status), job.Progress,
		job.AudioKey, job.VTTKey, job.OutputKey,
		job.AudioName, job.VTTName,
		job.Config.TTSProvider, job.Config.TTSVolume,
		job.Config.NoSpeedup, job.Config.Concurrency,
		job.CreatedAt, job.CompletedAt,
		job.Error,
	)
	if err != nil {
		return fmt.Errorf("insert job: %w", err)
	}
	return nil
}

func (r *PostgresJobRepo) FindByID(ctx context.Context, id string) (domain.Job, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, status, progress, audio_key, vtt_key, output_key,
		       audio_name, vtt_name,
		       tts_provider, tts_volume, no_speedup, concurrency,
		       created_at, completed_at, error
		FROM jobs WHERE id = $1
	`, id)
	return scanPgJob(row)
}

func (r *PostgresJobRepo) ListRecent(ctx context.Context, userID string, limit int) ([]domain.Job, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, status, progress, audio_key, vtt_key, output_key,
		       audio_name, vtt_name,
		       tts_provider, tts_volume, no_speedup, concurrency,
		       created_at, completed_at, error
		FROM jobs WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list jobs: %w", err)
	}
	defer rows.Close()

	var jobs []domain.Job
	for rows.Next() {
		job, err := scanPgJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *PostgresJobRepo) UpdateStatus(ctx context.Context, id string, status domain.Status, progress string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status = $1, progress = $2 WHERE id = $3
	`, string(status), progress, id)
	return checkAffected(res, err, id)
}

func (r *PostgresJobRepo) SetCompleted(ctx context.Context, id, outputKey string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status = $1, output_key = $2, completed_at = $3 WHERE id = $4
	`, string(domain.StatusCompleted), outputKey, time.Now(), id)
	return checkAffected(res, err, id)
}

func (r *PostgresJobRepo) SetFailed(ctx context.Context, id, errMsg string) error {
	res, err := r.db.ExecContext(ctx, `
		UPDATE jobs SET status = $1, error = $2, completed_at = $3 WHERE id = $4
	`, string(domain.StatusFailed), errMsg, time.Now(), id)
	return checkAffected(res, err, id)
}

func (r *PostgresJobRepo) DeleteExpired(ctx context.Context, ttl time.Duration) ([]domain.Job, error) {
	cutoff := time.Now().Add(-ttl)

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, status, progress, audio_key, vtt_key, output_key,
		       audio_name, vtt_name,
		       tts_provider, tts_volume, no_speedup, concurrency,
		       created_at, completed_at, error
		FROM jobs WHERE created_at < $1 AND status IN ($2, $3)
	`, cutoff, string(domain.StatusCompleted), string(domain.StatusFailed))
	if err != nil {
		return nil, fmt.Errorf("query expired: %w", err)
	}
	defer rows.Close()

	var expired []domain.Job
	for rows.Next() {
		job, err := scanPgJob(rows)
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
			DELETE FROM jobs WHERE created_at < $1 AND status IN ($2, $3)
		`, cutoff, string(domain.StatusCompleted), string(domain.StatusFailed))
		if err != nil {
			return nil, fmt.Errorf("delete expired: %w", err)
		}
	}

	return expired, nil
}

// scanPgJob scans a Postgres row into a domain.Job.
// Uses time.Time (TIMESTAMPTZ) and bool (BOOLEAN) instead of SQLite's string/int types.
func scanPgJob(s scanner) (domain.Job, error) {
	var (
		j           domain.Job
		status      string
		completedAt sql.NullTime
		errMsg      sql.NullString
	)

	err := s.Scan(
		&j.ID, &j.UserID, &status, &j.Progress,
		&j.AudioKey, &j.VTTKey, &j.OutputKey,
		&j.AudioName, &j.VTTName,
		&j.Config.TTSProvider, &j.Config.TTSVolume,
		&j.Config.NoSpeedup, &j.Config.Concurrency,
		&j.CreatedAt, &completedAt, &errMsg,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Job{}, domain.ErrJobNotFound
		}
		return domain.Job{}, fmt.Errorf("scan job: %w", err)
	}

	j.Status = domain.Status(status)
	if completedAt.Valid {
		t := completedAt.Time
		j.CompletedAt = &t
	}
	if errMsg.Valid {
		j.Error = &errMsg.String
	}

	return j, nil
}
