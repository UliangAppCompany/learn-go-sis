package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/time/rate"
	_ "modernc.org/sqlite"

	"sis/internal/auth"
	"sis/internal/db"
	"sis/internal/student"
	"sis/internal/tenant"
)

const schema = ` 
	CREATE table if not exists students (
		tenant_id string not null, 
		id 		integer not null, 
		name 	text not null, 
		gpa 	real not null, 
		primary key (tenant_id, id)
	); 
	
	CREATE table if not exists users (
		tenant_id TEXT not null, id integer not null, email text not null, password_hash text not null, primary key (tenant_id, id)
	); 

	CREATE table if not exists sessions (
		token text not null primary key, 
		tenant_id text not null, 
		user_id integer not null, 
		created_at text not null, 
		expires_at text not null
	)
`

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func sessionCookie(value string, secure bool, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "session",
		Value:    value,
		Path:     "/",
		Secure:   secure,
		HttpOnly: true,
		MaxAge:   maxAge,
		SameSite: http.SameSiteLaxMode,
	}
}

func main() {
	addr := ":" + getenv("PORT", "8080")
	secureCookies := getenv("COOKIES_SECURE", "false") == "true"
	dbPath := getenv("DATABASE_PATH", "sis.db")
	sqlDB, err := sql.Open("sqlite", dbPath)
	loginLimiter := newIpLimiter(rate.Every(time.Minute/5), 5)
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer sqlDB.Close()

	if _, err := sqlDB.Exec(schema); err != nil {
		log.Fatalf("schema: %v", err)
	}

	store := student.NewSqlcStore(sqlDB)
	queries := db.New(sqlDB)
	seedTenant(store, "springfield", []student.Student{
		{ID: 1, Name: "ada", GPA: 4.},
		{ID: 2, Name: "alan", GPA: 3.9},
	})
	seedTenant(store, "shelby", []student.Student{
		{ID: 1, Name: "Grace", GPA: 4.},
	})

	seedUser(queries, "springfield", 1, "ada@springfield.edu", "password123")

	requireLogin := requireSession(queries)

	mux := http.NewServeMux()
	handler := chain(mux, recoverPanic(logger), logRequests(logger), securityHeaders, limitBody(1<<20))

	mux.HandleFunc("GET /healthz", health)
	mux.Handle("POST /login", loginLimiter.middleware(login(queries, secureCookies)))
	mux.Handle("POST /logout", logout(queries, secureCookies))
	mux.Handle("GET /students", requireLogin(listStudents(store)))
	mux.Handle("GET /students/{id}", requireLogin(getStudent(store)))

	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startSessionSweeper(ctx, queries, logger)

	go func() {
		log.Printf("SIS Listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("stopped cleanly")
}

func health(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "ok")
}

func seedUser(queries *db.Queries, t string, userID int64, email string, plain string) {
	ctx := context.Background()
	hashed_password, err := auth.HashPassword(plain)
	if err != nil {
		log.Fatalf("seed %s: %v", email, err)
	}
	err = queries.CreateUser(ctx, db.CreateUserParams{
		TenantID:     t,
		Email:        email,
		PasswordHash: hashed_password,
		ID:           userID,
	})
	if err != nil {
		log.Fatalf("seed user %s: %v", email, err)
	}
}

func seedTenant(store student.Store, t tenant.ID, students []student.Student) {
	ctx := tenant.NewContext(context.Background(), t)
	if existing, _ := store.List(ctx); len(existing) > 0 {
		return
	}

	for _, s := range students {
		if err := store.Add(ctx, s); err != nil {
			log.Fatalf("seed %s: %v", t, err)
		}
	}
}

func getStudent(store student.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rawid := r.PathValue("id")
		id, err := strconv.ParseInt(rawid, 10, 64)
		if err != nil {
			http.Error(w, "Not a valid id", http.StatusBadRequest)
			return
		}

		student, err := store.Get(r.Context(), id)
		if err != nil {
			http.Error(w, "Student not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(student); err != nil {
			http.Error(w, "could not encode student", http.StatusInternalServerError)
		}
	}
}

func seed(store student.Store) {
	ctx := context.Background()
	if existing, _ := store.List(ctx); len(existing) > 0 {
		return
	}
	for _, s := range []student.Student{
		{ID: 1, Name: "Ada", GPA: 4.0},
		{ID: 2, Name: "Alan", GPA: 3.89},
	} {
		if err := store.Add(ctx, s); err != nil {
			log.Fatalf("seeding: %v", err)
		}
	}
}

func listStudents(store student.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		students, err := store.List(r.Context())
		if err != nil {
			http.Error(w, "could not list students", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(students); err != nil {
			http.Error(w, "could not encode students", http.StatusInternalServerError)
		}
	}
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func login(queries *db.Queries, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req loginRequest
		const sessionTTL = 7 * 24 * time.Hour
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
				return
			}
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}

		user, err := queries.GetUserByEmail(r.Context(), req.Email)
		if err != nil {
			http.Error(w, "invalid email or password", http.StatusUnauthorized)
			return
		}

		if err := auth.CheckPassword(user.PasswordHash, req.Password); err != nil {
			http.Error(w, "invalid email or password", http.StatusUnauthorized)
			return
		}

		token, err := auth.NewToken()
		if err != nil {
			http.Error(w, "could not create session", http.StatusInternalServerError)
		}
		now := time.Now().UTC()
		err = queries.CreateSession(r.Context(), db.CreateSessionParams{
			Token:     token,
			TenantID:  user.TenantID,
			UserID:    user.ID,
			CreatedAt: now.Format(time.RFC3339),
			ExpiresAt: now.Add(sessionTTL).Format(time.RFC3339),
		})
		if err != nil {
			http.Error(w, "could not create session", http.StatusInternalServerError)
		}

		http.SetCookie(w, sessionCookie(token, secure, 0))
		w.WriteHeader(http.StatusNoContent)

	}
}

type sessions interface {
	GetSession(ctx context.Context, token string) (db.Session, error)
}

func requireSession(s sessions) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			c, err := r.Cookie("session")
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			sess, err := s.GetSession(r.Context(), c.Value)
			if err != nil {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			exp, err := time.Parse(time.RFC3339, sess.ExpiresAt)
			if err != nil || time.Now().After(exp) {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			ctx := tenant.NewContext(r.Context(), tenant.ID(sess.TenantID))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func logout(queries *db.Queries, secure bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie("session"); err == nil {
			_ = queries.DeleteSession(r.Context(), c.Value)
		}
		http.SetCookie(w, sessionCookie("", secure, -1))
		w.WriteHeader(http.StatusNoContent)
	}
}

func startSessionSweeper(ctx context.Context, queries *db.Queries, logger *slog.Logger) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				now := time.Now().UTC().Format(time.RFC3339)
				if err := queries.DeleteExpiredSessions(context.Background(), now); err != nil {
					logger.Error("session sweep", "err", err)
				}
			}
		}
	}()
}
