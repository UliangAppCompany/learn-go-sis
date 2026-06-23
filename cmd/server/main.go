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

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver for goose
	"github.com/pressly/goose/v3"
	"golang.org/x/time/rate"

	"sis/db/migrations"
	"sis/internal/auth"
	"sis/internal/db"
	"sis/internal/student"
	"sis/internal/tenant"
)

// runMigrations applies the embedded migrations to bring the schema up to date.
// goose wants a *sql.DB, so we open one with the pgx stdlib driver just for this,
// then close it — the app itself runs on the pgxpool. Versions are tracked in a
// goose_db_version table, so each migration runs exactly once.
func runMigrations(dsn string) error {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer sqlDB.Close()

	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("dialect: %w", err)
	}
	// "." is the FS root — the embed holds the .sql files directly.
	if err := goose.Up(sqlDB, "."); err != nil {
		return fmt.Errorf("up: %w", err)
	}
	return nil
}

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
	port := getenv("PORT", "8080")
	addr := ":" + port
	secureCookies := getenv("COOKIES_SECURE", "false") == "true"
	dsn := getenv("DATABASE_URL", "postgres://sis_learn_go:dev@localhost:5434/sis_go_dev")
	loginLimiter := newIpLimiter(rate.Every(time.Minute/5), 5)
	// Label every log line with the instance, so a fleet of identical binaries
	// behind the load balancer is still legible in the aggregated stream.
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("instance", port)
	slog.SetDefault(logger)

	// One-shot subcommands, run as deploy steps and then exit. Keeping them OUT
	// of the serve path is what makes the serve path safe to run N times at once:
	// migrate and seed each touch shared state, and must happen exactly once, not
	// once per instance.
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "migrate":
			if err := runMigrations(dsn); err != nil {
				log.Fatalf("migrate: %v", err)
			}
			log.Println("migrations applied")
			return
		case "seed":
			if err := runSeed(dsn); err != nil {
				log.Fatalf("seed: %v", err)
			}
			log.Println("seed complete")
			return
		default:
			log.Fatalf("unknown subcommand %q (want: migrate, seed)", os.Args[1])
		}
	}

	// pgxpool.New parses the DSN and creates a connection pool. It does NOT
	// connect yet — the first query (or Ping) opens the first connection.
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(context.Background()); err != nil {
		log.Fatalf("ping db: %v", err)
	}

	// No writes on boot: the serve path is now side-effect free, so it's safe to
	// run on every instance. Demo data is loaded once via `sis seed`.
	store := student.NewSqlcStore(pool)
	queries := db.New(pool)

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
	if _, err := queries.GetUserByEmail(ctx, email); err == nil {
		return
	}
	hashed_password, err := auth.HashPassword(plain)
	if err != nil {
		slog.Error("seed user: hash", "email", email, "err", err)
		return
	}
	err = queries.CreateUser(ctx, db.CreateUserParams{
		TenantID:     t,
		Email:        email,
		PasswordHash: hashed_password,
		ID:           userID,
	})
	if err != nil {
		slog.Error("seed user", "email", email, "err", err)
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

// runSeed loads the demo tenants and user. It's a one-shot subcommand, not a
// boot step: with multiple instances, seeding on every serve would race two
// processes inserting the same rows. The guards inside seedTenant/seedUser make
// a repeat run safe.
func runSeed(dsn string) error {
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		return fmt.Errorf("open: %w", err)
	}
	defer pool.Close()

	store := student.NewSqlcStore(pool)
	queries := db.New(pool)
	seedTenant(store, "springfield", []student.Student{
		{ID: 1, Name: "ada", GPA: 4.},
		{ID: 2, Name: "alan", GPA: 3.9},
	})
	seedTenant(store, "shelby", []student.Student{
		{ID: 1, Name: "Grace", GPA: 4.},
	})
	seedUser(queries, "springfield", 1, "ada@springfield.edu", "password123")
	return nil
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
			CreatedAt: now,
			ExpiresAt: now.Add(sessionTTL),
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
			// ExpiresAt is a real timestamptz now — a time.Time straight from the
			// driver. No more RFC3339 round-trip through a TEXT column to parse.
			if time.Now().After(sess.ExpiresAt) {
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
				now := time.Now().UTC()
				if err := queries.DeleteExpiredSessions(context.Background(), now); err != nil {
					logger.Error("session sweep", "err", err)
				}
			}
		}
	}()
}
