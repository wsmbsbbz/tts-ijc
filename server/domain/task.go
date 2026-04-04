package domain

import (
	"context"
	"time"
)

// TaskSource identifies how a task was created.
type TaskSource string

const (
	TaskSourceNew TaskSource = "new" // single audio upload via /new
	TaskSourceRJ  TaskSource = "rj"  // asmr.one batch via /rj
)

// Task groups one or more Jobs created from a single user action.
// A /new task contains exactly one job; a /rj task contains one job per selected audio.
type Task struct {
	ID        string
	UserID    string
	Source    TaskSource
	Title     string    // audio filename (new) or work title (rj)
	Workno    string    // empty for new tasks; "RJ299717" etc. for rj tasks
	CreatedAt time.Time
}

// NewTask creates a Task with the given attributes.
func NewTask(id, userID string, source TaskSource, title, workno string) Task {
	return Task{
		ID:        id,
		UserID:    userID,
		Source:    source,
		Title:     title,
		Workno:    workno,
		CreatedAt: time.Now(),
	}
}

// TaskRepository defines persistence operations for Task entities.
type TaskRepository interface {
	Save(ctx context.Context, task Task) error
	FindByID(ctx context.Context, id string) (Task, error)
	ListRecent(ctx context.Context, userID string, limit int) ([]Task, error)
}
