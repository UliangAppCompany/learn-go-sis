package db_test

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"sis/internal/db"
)

func newTestQueries(t *testing.T) *db.Queries {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	sqlDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	const schema = `CREATE TABLE students (tenant_id string not null, id integer not null, name text not null, gpa real not null, primary key (tenant_id, id));`
	if _, err := sqlDB.Exec(schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	return db.New(sqlDB)
}

func TestCreateAndGetStudent(t *testing.T) {
	q := newTestQueries(t)
	ctx := context.Background()
	if err := q.CreateStudent(ctx, db.CreateStudentParams{TenantID: "stup", ID: 1, Name: "Ada", Gpa: 4.}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := q.GetStudent(ctx, db.GetStudentParams{ID: 1, TenantID: "stup"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Ada" {
		t.Errorf("Name = %q, want Ada", got.Name)
	}
}
