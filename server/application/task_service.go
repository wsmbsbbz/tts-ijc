package application

import (
	"context"
	"fmt"

	"github.com/wsmbsbbz/tts-ijc/server/domain"
)

// TaskService handles task creation and retrieval.
type TaskService struct {
	repo   domain.TaskRepository
	idFunc func() string
}

// NewTaskService creates a TaskService.
func NewTaskService(repo domain.TaskRepository, idFunc func() string) *TaskService {
	return &TaskService{repo: repo, idFunc: idFunc}
}

// CreateTask creates and persists a new Task.
func (s *TaskService) CreateTask(ctx context.Context, userID string, source domain.TaskSource, title, workno string) (domain.Task, error) {
	task := domain.NewTask(s.idFunc(), userID, source, title, workno)
	if err := s.repo.Save(ctx, task); err != nil {
		return domain.Task{}, fmt.Errorf("save task: %w", err)
	}
	return task, nil
}

// GetTask retrieves a single task by ID.
func (s *TaskService) GetTask(ctx context.Context, id string) (domain.Task, error) {
	return s.repo.FindByID(ctx, id)
}

// ListTasks returns the most recent tasks for the given user.
func (s *TaskService) ListTasks(ctx context.Context, userID string, limit int) ([]domain.Task, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.repo.ListRecent(ctx, userID, limit)
}
