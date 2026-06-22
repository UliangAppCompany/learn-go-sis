package student

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"sis/internal/db"
	"sis/internal/tenant"
)

type SqlcStore struct {
	q *db.Queries
}

func NewSqlcStore(sqlDB *sql.DB) *SqlcStore {
	return &SqlcStore{q: db.New(sqlDB)}
}

func (s *SqlcStore) Add(ctx context.Context, st Student) error {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		return fmt.Errorf("no tenant in context")
	}
	err := s.q.CreateStudent(ctx, db.CreateStudentParams{
		TenantID: string(tid),
		ID:       st.ID,
		Name:     st.Name,
		Gpa:      st.GPA,
	})
	if err != nil {
		return fmt.Errorf("creating student %d: %w", st.ID, err)
	}
	return nil
}
func (s *SqlcStore) Get(ctx context.Context, id int64) (Student, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		return Student{}, fmt.Errorf("no tenant in context")
	}
	row, err := s.q.GetStudent(ctx, db.GetStudentParams{ID: id, TenantID: string(tid)})
	if errors.Is(err, sql.ErrNoRows) {
		return Student{}, fmt.Errorf("student %d not found", id)
	}
	if err != nil {
		return Student{}, fmt.Errorf("getting student %d: %w", id, err)
	}
	return fromDB(row), nil
}

func (s *SqlcStore) List(ctx context.Context) ([]Student, error) {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		return nil, fmt.Errorf("no tenant in context")
	}
	rows, err := s.q.ListStudents(ctx, string(tid))
	if err != nil {
		return nil, fmt.Errorf("listing students: %w", err)
	}
	out := make([]Student, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromDB(r))
	}
	return out, nil
}

func fromDB(r db.Student) Student {
	return Student{ID: r.ID, Name: r.Name, GPA: r.Gpa}
}

var _ Store = (*SqlcStore)(nil)
