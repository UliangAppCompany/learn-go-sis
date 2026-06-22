package student

import (
	"context"
	"testing"

	"sis/internal/tenant"
)

func TestNewRequiresName(t *testing.T) {
	_, err := New(1, "")
	if err == nil {
		t.Fatal("expected an error for empty name, got nil")
	}
}

func TestNewValid(t *testing.T) {
	s, err := New(1, "Ada")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s.Name != "Ada" {
		t.Errorf("Name = %q, want %q", s.Name, "Ada")
	}
}

func TestMemStoreGet(t *testing.T) {
	store := NewMemStore()
	ctx := tenant.NewContext(context.Background(), "stup")
	store.Add(ctx, Student{ID: 1, Name: "Ada"})

	cases := []struct {
		name    string
		id      int64
		wantErr bool
	}{
		{"existing student", 1, false},
		{"missing student", 99, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := store.Get(ctx, tc.id)
			if (err != nil) != tc.wantErr {
				t.Errorf("Get(%d) error = %v, wantErr %v", tc.id, err, tc.wantErr)
			}
		})
	}

}

func TestTenantIsolation(t *testing.T) {
	store := NewMemStore()
	ctxA := tenant.NewContext(context.Background(), "springfield")
	ctxB := tenant.NewContext(context.Background(), "shelby")

	if err := store.Add(ctxA, Student{ID: 1, Name: "Ada"}); err != nil {
		t.Fatalf("add: %v", err)
	}
	if _, err := store.Get(ctxB, 1); err == nil {
		t.Fatalf("ISOLATION BREACH: tenant B read tenant A's student")
	}
	if list, _ := store.List(ctxB); len(list) != 0 {
		t.Errorf("tenant B sees %d students, want 0", len(list))
	}
	if _, err := store.Get(ctxA, 1); err != nil {
		t.Errorf("tenant A cannot see its own students: %v", err)
	}

}

func TestMissingTenantFailsClosed(t *testing.T) {
	store := NewMemStore()
	if _, err := store.List(context.Background()); err == nil {
		t.Fatalf("expected error with no tenant in context, got nil")
	}
}
