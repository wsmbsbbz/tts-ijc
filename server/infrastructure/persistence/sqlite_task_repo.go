package persistence

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// SQLiteTaskRepo implements domain.TaskRepository using SQLite.
type SQLiteTaskRepo struct {
	db *sql.DB
}

// NewSQLiteTaskRepo creates the tasks table if necessary and returns a repo.
func NewSQLiteTaskRepo(db *sql.DB) (*SQLiteTaskRepo, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL DEFAULT '',
			source     TEXT NOT NULL DEFAULT 'new',
			title      TEXT NOT NULL DEFAULT '',
			workno     TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create tasks table: %w", err)
	}
	return &SQLiteTaskRepo{db: db}, nil
}

func (r *SQLiteTaskRepo) Save(ctx context.Context, task domain.Task) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tasks (id, user_id, source, title, workno, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, task.ID, task.UserID, string(task.Source), task.Title, task.Workno,
		task.CreatedAt.Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

func (r *SQLiteTaskRepo) FindByID(ctx context.Context, id string) (domain.Task, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, source, title, workno, created_at
		FROM tasks WHERE id = ?
	`, id)
	return scanTask(row)
}

func (r *SQLiteTaskRepo) ListRecent(ctx context.Context, userID string, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, source, title, workno, created_at
		FROM tasks WHERE user_id = ? ORDER BY created_at DESC LIMIT ?
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []domain.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

type taskScanner interface {
	Scan(dest ...any) error
}

func scanTask(s taskScanner) (domain.Task, error) {
	var (
		t         domain.Task
		source    string
		createdAt string
	)
	err := s.Scan(&t.ID, &t.UserID, &source, &t.Title, &t.Workno, &createdAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Task{}, fmt.Errorf("task not found")
		}
		return domain.Task{}, fmt.Errorf("scan task: %w", err)
	}
	t.Source = domain.TaskSource(source)
	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	return t, nil
}
