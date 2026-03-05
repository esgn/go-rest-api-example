// main is the composition root of the application.
//
// "Composition root" means this is where we wire concrete implementations:
// HTTP server + strict OpenAPI adapter + middleware + service + repository + DB.
// No business logic should live here; main only assembles and starts the app.
//
// Request workflow (runtime order):
//  1. Generated router matches method/path to an operation.
//  2. Generated pre-middleware binding runs for path/query params (when present).
//     Example: GET /notes?limit=abc fails here before custom middleware runs.
//     This step does binding/coercion (required + type/format), not schema constraints
//     like maxLength/min/max/enum semantics.
//  3. Std middleware chain runs (runtime: RequestLogger -> body/content-type guard
//     -> unknown-query guard -> query-rule guard -> unknown-JSON-field guard).
//  4. Strict adapter builds typed request objects; for write operations it decodes JSON body.
//  5. API handler translates typed request DTOs to service calls.
//  6. Service enforces business rules and orchestrates use cases.
//  7. Repository persists/fetches data through DB layer.
//  8. Handler maps domain errors to HTTP responses.
//  9. Strict adapter writes typed responses.
//  10. Errors from step 2 go to router ErrorHandlerFunc; errors from steps 4/9 go to
//     strict request/response error handlers.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	handlers "notes-api/internal/http"
	"notes-api/internal/http/middleware"
	gen "notes-api/internal/http/openapi"
	"notes-api/internal/notes/repository"
	"notes-api/internal/notes/service"
	"notes-api/internal/platform/db"
)

var appLogLevel = new(slog.LevelVar)

func main() {
	// Load .env when present (local dev convenience).
	// Missing .env is fine: environment variables still come from the process.
	_ = godotenv.Load()
	configureLogLevel()

	// Transport/infrastructure configuration.
	httpAddr := envOrDefault("HTTP_ADDR", ":8080")
	sqlitePath := envOrDefault("SQLITE_PATH", "./notes.db")

	// Database pool settings (operational knobs, not business rules).
	dbCfg := db.DefaultConfig()
	dbCfg.MaxOpenConns = envOrDefaultInt("DB_MAX_OPEN_CONNS", dbCfg.MaxOpenConns)
	dbCfg.MaxIdleConns = envOrDefaultInt("DB_MAX_IDLE_CONNS", dbCfg.MaxIdleConns)

	// Service config controls business limits (content length, page limits, etc.).
	svcCfg, err := loadServiceConfigFromEnv()
	if err != nil {
		log.Fatalf("invalid service config: %v", err)
	}

	// Context cancelled on SIGINT/SIGTERM for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Open database and fail fast if unreachable/misconfigured.
	database, err := db.OpenSQLite(ctx, sqlitePath, dbCfg)
	if err != nil {
		log.Fatalf("database startup failed: %v", err)
	}

	// Ensure SQL connection is closed on process exit.
	sqlDB, err := database.DB()
	if err != nil {
		log.Fatalf("database init failed: %v", err)
	}
	defer sqlDB.Close()

	// Run schema migration at startup
	// In production, a separate migration process is often preferred
	if err := db.Migrate(ctx, database); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}

	// Build the layered dependencies:
	// repository -> service -> strict HTTP handlers.
	noteRepo := repository.NewSQLiteNotesRepository(database)
	// passing noteRepo into that function forces the compiler to verify interface compatibility at startup
	// so we get a compile-time error if the repo doesn't satisfy the service's expected interface.
	noteService := service.NewNotesService(noteRepo, svcCfg)
	noteHandlers := handlers.NewNotesHandler(noteService)

	// Keep transport validation limits centralized in middleware package.
	maxBodyBytes := middleware.MaxBodyBytesForMaxContentLength(svcCfg.MaxContentLength)
	maxAfterCursorLength := middleware.DefaultMaxAfterCursorLength

	// Strict handler validates/coerces request/response objects generated from OpenAPI.
	strictServer := gen.NewStrictHandlerWithOptions(noteHandlers, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Warn("strict_request_error",
				"method", r.Method,
				"path", r.URL.Path,
				"err", err.Error(),
			)
			writeJSONError(w, http.StatusBadRequest, err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("strict_response_error",
				"method", r.Method,
				"path", r.URL.Path,
				"err", err.Error(),
			)
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		},
	})

	// Build the net/http server with generated router + focused validation middleware.
	// NOTE: generated handler middleware order is reverse-wrapped, so registration
	// order here is intentionally reverse of runtime execution order.
	server := &http.Server{
		Addr: httpAddr,
		Handler: gen.HandlerWithOptions(strictServer, gen.StdHTTPServerOptions{
			// Registered in reverse of runtime execution; together these enforce
			// transport-level validation and request logging.
			Middlewares: []gen.MiddlewareFunc{
				middleware.RejectUnknownJSONFields(),
				middleware.EnforceQueryRules(maxAfterCursorLength, svcCfg.MaxPageLimit),
				middleware.RejectUnknownQueryParams(),
				middleware.EnforceBodyAndContentType(maxBodyBytes),
				middleware.RequestLogger(),
			},
			// Handles generated router/binding failures before handlers run.
			// Contract stays a 400 JSON error payload.
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				slog.Warn("router_bind_error",
					"method", r.Method,
					"path", r.URL.Path,
					"err", err.Error(),
				)
				writeJSONError(w, http.StatusBadRequest, err.Error())
			},
		}),
		// Header timeout limits slow header delivery; read/write timeouts bound
		// full request I/O; idle timeout limits keep-alive connection idleness.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Shutdown hook: when context is cancelled, stop accepting new requests and
	// allow in-flight requests up to 10s to finish.
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("http_shutdown_error", "err", err.Error())
		}
	}()

	slog.Info("notes_api_listening", "http_addr", httpAddr, "sqlite_path", sqlitePath)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server failed: %v", err)
	}
}

func configureLogLevel() {
	raw := envOrDefault("LOG_LEVEL", "info")
	level, err := parseLogLevel(raw)
	if err != nil {
		level = slog.LevelInfo
	}

	appLogLevel.Set(level)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: appLogLevel,
	})))

	if err != nil {
		slog.Warn("invalid_log_level", "value", raw, "fallback", "info")
	}

	slog.Info("log_level_configured", "log_level", strings.ToUpper(level.String()))
}

// envOrDefault returns the environment value when set, otherwise fallback.
func envOrDefault(name, fallback string) string {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	return value
}

// loadServiceConfigFromEnv loads business-rule config from env with defaults.
// It validates the final values so the process fails fast on bad configuration.
func loadServiceConfigFromEnv() (service.Config, error) {
	cfg := service.DefaultConfig()
	cfg.MaxContentLength = envOrDefaultInt("NOTE_MAX_CONTENT_LENGTH", cfg.MaxContentLength)
	cfg.MaxTitleLength = envOrDefaultInt("NOTE_MAX_TITLE_LENGTH", cfg.MaxTitleLength)
	cfg.DefaultPageLimit = envOrDefaultInt("PAGE_DEFAULT_LIMIT", cfg.DefaultPageLimit)
	cfg.MaxPageLimit = envOrDefaultInt("PAGE_MAX_LIMIT", cfg.MaxPageLimit)

	if err := service.ValidateConfig(cfg); err != nil {
		return service.Config{}, err
	}
	return cfg, nil
}

// envOrDefaultInt parses an integer env var with fallback on empty/invalid input.
// Invalid values log a warning and do not stop startup.
func envOrDefaultInt(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		slog.Warn("invalid_integer_env", "env", name, "value", value, "fallback", fallback)
		return fallback
	}
	return parsed
}

func parseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, errors.New("invalid log level")
	}
}

// writeJSONError keeps error responses consistent across startup/router hooks.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
