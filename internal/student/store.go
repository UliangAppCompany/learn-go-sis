package student

import (
	"context"
	"fmt"

	"sis/internal/tenant"
)

type Store interface {
	Add(ctx context.Context, s Student) error
	Get(ctx context.Context, id int64) (Student, error)
	List(ctx context.Context) ([]Student, error)
}

type MemStore struct {
	index map[tenant.ID]map[int64]Student
}

func NewMemStore() *MemStore {
	return &MemStore{index: make(map[tenant.ID]map[int64]Student)}
}

func (m *MemStore) Add(ctx context.Context, s Student) error {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		return fmt.Errorf("no tenant in context")
	}
	if m.index[tid] == nil {
		m.index[tid] = make(map[int64]Student)
	}
	m.index[tid][s.ID] = s
	return nil
}

func (m *MemStore) Get(ctx context.Context, id int64) (Student, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		return Student{}, fmt.Errorf("no tenant in context")
	}
	s, ok := m.index[tid][id]
	if !ok {
		return Student{}, fmt.Errorf("student %d not found", id)
	}
	return s, nil
}

func (m *MemStore) List(ctx context.Context) ([]Student, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no tenant in context")
	}
	out := []Student{}
	for _, s := range m.index[tid] {
		out = append(out, s)
	}
	return out, nil
}

var _ Store = (*MemStore)(nil)
