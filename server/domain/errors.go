package domain

import "errors"

var (
	ErrJobNotFound = errors.New("job not found")
	ErrQueueFull   = errors.New("job queue is full")
)
