package persistence

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteMigrateFilterOnomatopoeiaDefaultFalse(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "jobs.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE jobs (
			id           TEXT PRIMARY KEY,
			user_id      TEXT NOT NULL DEFAULT '',
			task_id      TEXT NOT NULL DEFAULT '',
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
			output_size  INTEGER NOT NULL DEFAULT 0,
			created_at   TEXT NOT NULL,
			completed_at TEXT,
			error        TEXT
		)
	`)
	if err != nil {
		t.Fatalf("create legacy table: %v", err)
	}

	if err := migrateTable(db); err != nil {
		t.Fatalf("migrate table: %v", err)
	}

	createdAt := time.Now().Format(time.RFC3339)
	_, err = db.Exec(`
		INSERT INTO jobs (
			id, user_id, task_id, status, progress, audio_key, vtt_key, output_key,
			audio_name, vtt_name, tts_provider, tts_volume, no_speedup, concurrency,
			output_size, created_at, completed_at, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		"job-1", "u1", "t1", "queued", "", "a", "v", "",
		"a.mp3", "a.vtt", "edge", 0.08, 0, 3,
		0, createdAt, nil, nil,
	)
	if err != nil {
		t.Fatalf("insert row: %v", err)
	}

	repo := &SQLiteJobRepo{db: db}
	job, err := repo.FindByID(context.Background(), "job-1")
	if err != nil {
		t.Fatalf("find job: %v", err)
	}
	if job.Config.FilterOnomatopoeia {
		t.Fatalf("expected migrated default filter_onomatopoeia=false")
	}
}
