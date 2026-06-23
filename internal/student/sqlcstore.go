package student

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"sis/internal/db"
	"sis/internal/tenant"
)

type SqlcStore struct {
	pool *pgxpool.Pool
}

// NewSqlcStore holds the pool directly: each operation opens its own
// transaction so it can SET the tenant for Row-Level Security (see withTenant).
func NewSqlcStore(pool *pgxpool.Pool) *SqlcStore {
	return &SqlcStore{pool: pool}
}

// withTenant runs fn inside a transaction whose app.tenant_id is set to the
// caller's tenant. The database's RLS policy reads that setting, so every query
// in fn sees only this tenant's rows — even one that forgets a WHERE clause.
//
// set_config(..., true) is transaction-local: it resets on commit/rollback, so
// the setting can never leak to the next request that borrows this pooled
// connection. We use set_config (a function) rather than SET because SET cannot
// take a bind parameter — string-building a tenant into SET would be injectable.
func (s *SqlcStore) withTenant(ctx context.Context, fn func(*db.Queries, tenant.ID) error) error {
	tid, ok := tenant.FromContext(ctx)
	if !ok {
		return fmt.Errorf("no tenant in context")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once committed; the safety net if fn fails

	if _, err := tx.Exec(ctx, "select set_config('app.tenant_id', $1, true)", string(tid)); err != nil {
		return fmt.Errorf("set tenant: %w", err)
	}
	if err := fn(db.New(tx), tid); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *SqlcStore) Add(ctx context.Context, st Student) error {
	return s.withTenant(ctx, func(q *db.Queries, tid tenant.ID) error {
		err := q.CreateStudent(ctx, db.CreateStudentParams{
			TenantID: string(tid),
			ID:       st.ID,
			Name:     st.Name,
			Gpa:      st.GPA,
		})
		if err != nil {
			return fmt.Errorf("creating student %d: %w", st.ID, err)
		}
		return nil
	})
}

func (s *SqlcStore) Get(ctx context.Context, id int64) (Student, error) {
	var out Student
	err := s.withTenant(ctx, func(q *db.Queries, tid tenant.ID) error {
		row, err := q.GetStudent(ctx, db.GetStudentParams{ID: id, TenantID: string(tid)})
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("student %d not found", id)
		}
		if err != nil {
			return fmt.Errorf("getting student %d: %w", id, err)
		}
		out = fromDB(row)
		return nil
	})
	return out, err
}

func (s *SqlcStore) List(ctx context.Context) ([]Student, error) {
	var out []Student
	err := s.withTenant(ctx, func(q *db.Queries, tid tenant.ID) error {
		rows, err := q.ListStudents(ctx, string(tid))
		if err != nil {
			return fmt.Errorf("listing students: %w", err)
		}
		out = make([]Student, 0, len(rows))
		for _, r := range rows {
			out = append(out, fromDB(r))
		}
		return nil
	})
	return out, err
}

func fromDB(r db.Student) Student {
	return Student{ID: r.ID, Name: r.Name, GPA: r.Gpa}
}

var _ Store = (*SqlcStore)(nil)
