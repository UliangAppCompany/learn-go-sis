package db_test

import (
	"context"
	"database/sql"
	"log"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"sis/db/migrations"
	"sis/internal/db"
)

// TestMain migrates the test database once (goose tracks versions, so this is a
// no-op on an already-current DB), then runs the suite. Each test rolls back its
// own data; the schema persists. Skips migrating when TEST_DATABASE_URL is unset
// — the individual tests then skip via newTestTx.
func TestMain(m *testing.M) {
	if url := os.Getenv("TEST_DATABASE_URL"); url != "" {
		sqlDB, err := sql.Open("pgx", url)
		if err != nil {
			log.Fatalf("open test db: %v", err)
		}
		goose.SetBaseFS(migrations.FS)
		if err := goose.SetDialect("postgres"); err != nil {
			log.Fatalf("dialect: %v", err)
		}
		if err := goose.Up(sqlDB, "."); err != nil {
			log.Fatalf("migrate test db: %v", err)
		}
		sqlDB.Close()
	}
	os.Exit(m.Run())
}

// newTestTx returns a transaction rolled back when the test ends. The schema
// already exists (migrated in TestMain); only data needs cleaning, and the
// rollback handles that. Skips unless TEST_DATABASE_URL is set.
func newTestTx(t *testing.T) (context.Context, pgx.Tx) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to run db integration tests")
	}
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return ctx, tx
}

// setTenant points the RLS policy at one tenant for the rest of the transaction.
func setTenant(t *testing.T, ctx context.Context, tx pgx.Tx, id string) {
	t.Helper()
	if _, err := tx.Exec(ctx, "select set_config('app.tenant_id', $1, true)", id); err != nil {
		t.Fatalf("set tenant: %v", err)
	}
}

func TestCreateAndGetStudent(t *testing.T) {
	ctx, tx := newTestTx(t)
	setTenant(t, ctx, tx, "stub")
	q := db.New(tx)

	if err := q.CreateStudent(ctx, db.CreateStudentParams{TenantID: "stub", ID: 1, Name: "Ada", Gpa: 4.}); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := q.GetStudent(ctx, db.GetStudentParams{ID: 1, TenantID: "stub"})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Ada" {
		t.Errorf("Name = %q, want Ada", got.Name)
	}
}

// TestTenantIsolation is the whole point of RLS: a query with NO tenant filter
// still returns only the current tenant's rows, because the database enforces
// it. We seed two tenants, then read with a raw WHERE-less SELECT.
func TestTenantIsolation(t *testing.T) {
	ctx, tx := newTestTx(t)
	q := db.New(tx)

	setTenant(t, ctx, tx, "springfield")
	if err := q.CreateStudent(ctx, db.CreateStudentParams{TenantID: "springfield", ID: 1, Name: "Ada", Gpa: 4.}); err != nil {
		t.Fatalf("seed springfield: %v", err)
	}

	setTenant(t, ctx, tx, "shelby")
	if err := q.CreateStudent(ctx, db.CreateStudentParams{TenantID: "shelby", ID: 1, Name: "Grace", Gpa: 4.}); err != nil {
		t.Fatalf("seed shelby: %v", err)
	}

	// Back to springfield, and read with a deliberately tenant-blind query.
	setTenant(t, ctx, tx, "springfield")
	rows, err := tx.Query(ctx, "select tenant_id, name from students")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var tid, name string
		if err := rows.Scan(&tid, &name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if tid != "springfield" {
			t.Errorf("RLS leak: saw tenant %q in a springfield context", tid)
		}
		names = append(names, name)
	}
	if len(names) != 1 || names[0] != "Ada" {
		t.Errorf("got %v, want [Ada] only — RLS should hide shelby", names)
	}
}

// TestCrossTenantInsertRejected proves the WITH CHECK half of the policy: you
// cannot write a row for a tenant other than the one in app.tenant_id.
func TestCrossTenantInsertRejected(t *testing.T) {
	ctx, tx := newTestTx(t)
	q := db.New(tx)

	setTenant(t, ctx, tx, "springfield")
	err := q.CreateStudent(ctx, db.CreateStudentParams{TenantID: "shelby", ID: 99, Name: "Mallory", Gpa: 4.})
	if err == nil {
		t.Fatal("expected RLS to reject inserting a shelby row under springfield, got nil")
	}
}
