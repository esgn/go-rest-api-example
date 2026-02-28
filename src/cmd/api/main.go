// Package main is the entry point of the Notes API application.
//
// This file is responsible for "wiring" all the layers of the application together:
//   - Database connection (SQLite via GORM)
//   - Repository (data access)
//   - Service (business logic)
//   - HTTP handlers (receiving and responding to HTTP requests)
//   - HTTP server (listening for incoming requests)
//
// Think of it as the "assembly line" — it creates each piece and plugs them together.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"encoding/json"

	"github.com/joho/godotenv" // Loads .env file into os.Getenv

	// We import each layer of our application.
	// The "alias handlers" lets us call it "handlers" instead of "api", so it
	// doesn't conflict with the "gen" package (which is also named "api").
	handlers "notes-api/internal/api" // HTTP handler layer (translates HTTP into service calls)
	"notes-api/internal/db"           // Database initialization (opens SQLite, runs migrations)
	gen "notes-api/internal/gen"      // Auto-generated code from the OpenAPI spec (routes + models)
	"notes-api/internal/repository"   // Repository layer (reads/writes data using GORM)
	"notes-api/internal/service"      // Service layer (business rules + validation)
)

func main() {
	// ── Load .env file (optional) ─────────────────────────────────────────
	// godotenv.Load() reads a .env file and sets the values as environment
	// variables. If the file doesn't exist, we silently continue — in
	// production the env vars are set by the deployment environment.
	_ = godotenv.Load()

	// ── Configuration ────────────────────────────────────────────────────
	// Read configuration from environment variables, or fall back to defaults.
	// Values can also be provided via a .env file (loaded above).
	httpAddr := envOrDefault("HTTP_ADDR", ":8080")          // Address the server listens on (e.g. ":8080")
	sqlitePath := envOrDefault("SQLITE_PATH", "./notes.db") // Path to the SQLite database file

	// Build per-package configs from env vars, starting from each package's
	// DefaultConfig() and overriding with env values where provided.
	dbCfg := db.DefaultConfig()
	dbCfg.MaxOpenConns = envOrDefaultInt("DB_MAX_OPEN_CONNS", dbCfg.MaxOpenConns)
	dbCfg.MaxIdleConns = envOrDefaultInt("DB_MAX_IDLE_CONNS", dbCfg.MaxIdleConns)

	svcCfg := service.DefaultConfig()
	svcCfg.MaxContentLength = envOrDefaultInt("NOTE_MAX_CONTENT_LENGTH", svcCfg.MaxContentLength)
	svcCfg.MaxTitleLength = envOrDefaultInt("NOTE_MAX_TITLE_LENGTH", svcCfg.MaxTitleLength)
	svcCfg.DefaultPageLimit = envOrDefaultInt("PAGE_DEFAULT_LIMIT", svcCfg.DefaultPageLimit)
	svcCfg.MaxPageLimit = envOrDefaultInt("PAGE_MAX_LIMIT", svcCfg.MaxPageLimit)

	// ── Graceful shutdown setup ──────────────────────────────────────────
	// signal.NotifyContext creates a context that is automatically cancelled
	// when the process receives an interrupt signal (Ctrl+C) or a SIGTERM
	// (sent by Docker, Kubernetes, etc. when stopping a container).
	// This lets us shut down the server cleanly instead of just crashing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop() // Release signal resources when main() exits

	// ── Database ─────────────────────────────────────────────────────────
	// Open a connection to our SQLite database using GORM (an ORM library).
	// "database" is a *gorm.DB — the main object we use to interact with the DB.
	database, err := db.OpenSQLite(ctx, sqlitePath, dbCfg)
	if err != nil {
		log.Fatalf("database startup failed: %v", err) // log.Fatalf prints and exits the program
	}

	// Get the underlying *sql.DB (Go's standard database handle) so we can
	// close it when the program exits. This releases the database file lock.
	sqlDB, err := database.DB()
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	defer sqlDB.Close() // "defer" means this runs when main() returns

	// Run database migrations: this creates or updates the "notes" table
	// to match our model.Note struct. Safe to run every startup.
	if err := db.Migrate(ctx, database); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}

	// ── Dependency injection (wiring layers together) ────────────────────
	// This is where we create each layer and pass its dependency into the next.
	// The flow is:  database → repository → service → handlers
	//
	// Each layer only knows about the one directly below it (via an interface),
	// so they stay loosely coupled and easy to test independently.
	noteRepo := repository.NewNotesRepository(database)      // Repo needs the DB connection
	noteService := service.NewNotesService(noteRepo, svcCfg) // Service needs a store + config

	// Derive a body-size cap from the service config: worst-case UTF-8 is 4
	// bytes per rune, plus generous overhead for JSON framing (512 bytes).
	// This rejects over-sized bodies before any heap allocation for content.
	maxBodyBytes := int64(svcCfg.MaxContentLength)*4 + 512
	noteHandlers := handlers.NewNotesHandler(noteService, maxBodyBytes) // Handlers need the service

	// ── HTTP server ──────────────────────────────────────────────────────
	// gen.HandlerWithOptions() creates an http.Handler with routes matching the
	// OpenAPI spec and a custom error handler that returns JSON (consistent with
	// all other error responses). The default handler would return plain text.
	// This ensures parse errors (e.g. ?limit=xxx) return {"error": "..."} too.
	server := &http.Server{
		Addr: httpAddr, // e.g. ":8080"
		Handler: gen.HandlerWithOptions(noteHandlers, gen.StdHTTPServerOptions{
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			},
		}),
		// Timeouts prevent slow or malicious clients from holding connections
		// open indefinitely (Slowloris-style attacks, resource exhaustion).
		ReadHeaderTimeout: 5 * time.Second,  // time to receive the full request header
		ReadTimeout:       10 * time.Second, // time to read the entire request (header + body)
		WriteTimeout:      10 * time.Second, // time to write the full response
		IdleTimeout:       60 * time.Second, // time an idle keep-alive connection stays open
	}

	// ── Graceful shutdown goroutine ──────────────────────────────────────
	// A goroutine is a lightweight thread. Here we launch one that waits for
	// the shutdown signal (ctx.Done()), then tells the server to finish
	// ongoing requests within 10 seconds before forcefully closing.
	go func() {
		<-ctx.Done() // Block until we receive SIGINT or SIGTERM
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("http shutdown error: %v", err)
		}
	}()

	// ── Start listening ──────────────────────────────────────────────────
	// ListenAndServe blocks (waits) until the server is shut down.
	// When the graceful shutdown above calls server.Shutdown(), ListenAndServe
	// returns http.ErrServerClosed — which is expected, not a real error.
	log.Printf("notes-api listening on %s (sqlite=%s)", httpAddr, sqlitePath)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server failed: %v", err)
	}
}

// envOrDefault reads an environment variable by name.
// If the variable is not set (empty string), it returns the fallback value.
// Example: envOrDefault("HTTP_ADDR", ":8080") → returns ":8080" if HTTP_ADDR is not set.
func envOrDefault(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

// envOrDefaultInt reads an environment variable and parses it as an integer.
// If the variable is not set or cannot be parsed, it returns the fallback value.
// Example: envOrDefaultInt("DB_MAX_OPEN_CONNS", 10) → returns 10 if not set.
func envOrDefaultInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		log.Printf("WARNING: invalid integer for %s=%q, using default %d", name, value, fallback)
		return fallback
	}
	return parsed
}
