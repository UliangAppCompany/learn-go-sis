package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sis/internal/db"
	"sis/internal/tenant"
	"testing"
	"time"
)

type fakeSessions struct {
	byToken map[string]db.Session
}

func (f fakeSessions) GetSession(ctx context.Context, token string) (db.Session, error) {
	s, ok := f.byToken[token]
	if !ok {
		return db.Session{}, fmt.Errorf("not found")
	}
	return s, nil
}

func TestRequireSession(t *testing.T) {
	fake := fakeSessions{byToken: map[string]db.Session{
		"good-token": {Token: "good-token", TenantID: "springfield", UserID: 1, ExpiresAt: time.Now().Add(time.Hour)},
	}}
	probe := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tid, _ := tenant.FromContext(r.Context())
		fmt.Fprint(w, tid)
	})
	h := requireSession(fake)(probe)

	t.Run("no cookie -> 401", func(t *testing.T) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/students", nil))
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("valid session -> tenant from session", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/students", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "good-token"})
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != "springfield" {
			t.Errorf("got %d, %q, want 200 springfield", rec.Code, rec.Body.String())
		}
	})

	t.Run("expired session -> 401", func(t *testing.T) {
		fake := fakeSessions{byToken: map[string]db.Session{
			"old": {Token: "old", TenantID: "springfield", ExpiresAt: time.Now().Add(-time.Hour)},
		}}
		h := requireSession(fake)(probe)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/students", nil)
		req.AddCookie(&http.Cookie{Name: "session", Value: "old"})
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", rec.Code)
		}

	})
}
