package persistence

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// PostgresTaskRepo implements domain.TaskRepository using PostgreSQL.
type PostgresTaskRepo struct {
	db *sql.DB
}

// NewPostgresTaskRepo creates the tasks table if necessary and returns a repo.
func NewPostgresTaskRepo(db *sql.DB) (*PostgresTaskRepo, error) {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id         TEXT PRIMARY KEY,
			user_id    TEXT NOT NULL DEFAULT '',
			source     TEXT NOT NULL DEFAULT 'new',
			title      TEXT NOT NULL DEFAULT '',
			workno     TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMPTZ NOT NULL
		)
	`)
	if err != nil {
		return nil, fmt.Errorf("create tasks table: %w", err)
	}
	return &PostgresTaskRepo{db: db}, nil
}

func (r *PostgresTaskRepo) Save(ctx context.Context, task domain.Task) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO tasks (id, user_id, source, title, workno, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, task.ID, task.UserID, string(task.Source), task.Title, task.Workno, task.CreatedAt)
	if err != nil {
		return fmt.Errorf("insert task: %w", err)
	}
	return nil
}

func (r *PostgresTaskRepo) FindByID(ctx context.Context, id string) (domain.Task, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, user_id, source, title, workno, created_at
		FROM tasks WHERE id = $1
	`, id)
	return scanPgTask(row)
}

func (r *PostgresTaskRepo) ListRecent(ctx context.Context, userID string, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, source, title, workno, created_at
		FROM tasks WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2
	`, userID, limit)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []domain.Task
	for rows.Next() {
		t, err := scanPgTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

type pgTaskScanner interface {
	Scan(dest ...any) error
}

func scanPgTask(s pgTaskScanner) (domain.Task, error) {
	var (
		t      domain.Task
		source string
	)
	err := s.Scan(&t.ID, &t.UserID, &source, &t.Title, &t.Workno, &t.CreatedAt)
	if err != nil {
		if err == sql.ErrNoRows {
			return domain.Task{}, fmt.Errorf("task not found")
		}
		return domain.Task{}, fmt.Errorf("scan task: %w", err)
	}
	t.Source = domain.TaskSource(source)
	return t, nil
}
