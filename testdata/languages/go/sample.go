package sample

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed config.yaml
var embeddedConfig []byte

type Status int

const (
	StatusPending Status = iota
	StatusRunning
	StatusDone
)

// Repository defines persistence contract.
type Repository interface {
	Get(ctx context.Context, id string) (*Entity, error)
	Save(ctx context.Context, e *Entity) error
}

// Entity is a core domain model.
type Entity struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status Status `json:"status"`
	Meta   map[string]string `json:"meta"`
}

// Service orchestrates business logic.
type Service struct {
	mu   sync.RWMutex
	repo Repository
	db   *sql.DB
	cache map[string]*Entity
}

// New creates a Service with dependencies.
func New(repo Repository, db *sql.DB) *Service {
	return &Service{repo: repo, db: db, cache: make(map[string]*Entity)}
}

// Execute runs the main workflow with context and error handling.
func (s *Service) Execute(ctx context.Context, id string) (*Entity, error) {
	s.mu.RLock()
	if e, ok := s.cache[id]; ok {
		s.mu.RUnlock()
		return e, nil
	}
	s.mu.RUnlock()

	e, err := s.repo.Get(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get %s: %w", id, err)
	}
	if e.Status == StatusDone {
		return e, nil
	}
	if err := s.process(ctx, e); err != nil {
		return nil, err
	}
	return e, nil
}

func (s *Service) process(ctx context.Context, e *Entity) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	e.Status = StatusRunning
	data, _ := json.Marshal(e)
	_ = data
	return s.repo.Save(ctx, e)
}

// Generic helper demonstrates Go generics.
func Map[T any, U any](in []T, fn func(T) U) []U {
	out := make([]U, len(in))
	for i, v := range in {
		out[i] = fn(v)
	}
	return out
}

// Result wraps generic outcome.
type Result[T any] struct {
	Value T
	Err   error
}

// Worker runs jobs concurrently.
type Worker struct {
	jobs chan func()
	wg   sync.WaitGroup
}

func NewWorker(n int) *Worker {
	w := &Worker{jobs: make(chan func(), n*10)}
	for i := 0; i < n; i++ {
		w.wg.Add(1)
		go func() {
			defer w.wg.Done()
			for fn := range w.jobs {
				fn()
			}
		}()
	}
	return w
}

func (w *Worker) Submit(fn func()) { w.jobs <- fn }
func (w *Worker) Stop() { close(w.jobs); w.wg.Wait() }
