// main is the composition root of the application.
//
// "Composition root" means this is where we wire concrete implementations:
// HTTP server + strict OpenAPI adapter + middleware + service + repository + DB.
// No business logic should live here; main only assembles and starts the app.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"

	handlers "notes-api/internal/api"
	"notes-api/internal/api/middleware"
	"notes-api/internal/db"
	gen "notes-api/internal/gen"
	"notes-api/internal/logx"
	"notes-api/internal/repository"
	"notes-api/internal/service"
)

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

	// Run schema migration at startup.
	if err := db.Migrate(ctx, database); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}

	// Build the layered dependencies:
	// repository -> service -> strict HTTP handlers.
	noteRepo := repository.NewNotesRepository(database)
	noteService := service.NewNotesService(noteRepo, svcCfg)
	noteHandlers := handlers.NewNotesHandler(noteService)

	// Keep request-size cap aligned with old JSON decoder behavior:
	// max bytes ~= max UTF-8 payload size for content + tiny JSON overhead.
	maxBodyBytes := int64(svcCfg.MaxContentLength)*4 + 512
	maxAfterCursorLength := 512

	// Strict handler validates/coerces request/response objects generated from OpenAPI.
	strictServer := gen.NewStrictHandlerWithOptions(noteHandlers, nil, gen.StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logx.Warnf("msg=%q method=%s path=%s err=%q", "strict_request_error", r.Method, r.URL.Path, err.Error())
			writeJSONError(w, http.StatusBadRequest, err.Error())
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			logx.Errorf("msg=%q method=%s path=%s err=%q", "strict_response_error", r.Method, r.URL.Path, err.Error())
			writeJSONError(w, http.StatusInternalServerError, "internal server error")
		},
	})

	// Build the net/http server with generated router + focused validation middleware.
	// NOTE: generated handler middleware order is reverse-wrapped, so registration
	// order here is intentionally reverse of runtime execution order.
	server := &http.Server{
		Addr: httpAddr,
		Handler: gen.HandlerWithOptions(strictServer, gen.StdHTTPServerOptions{
			Middlewares: []gen.MiddlewareFunc{
				middleware.RejectUnknownJSONFields(),
				middleware.EnforceQueryRules(maxAfterCursorLength, svcCfg.MaxPageLimit),
				middleware.RejectUnknownQueryParams(),
				middleware.EnforceBodyAndContentType(maxBodyBytes),
				middleware.RequestLogger(),
			},
			ErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
				logx.Warnf("msg=%q method=%s path=%s err=%q", "router_bind_error", r.Method, r.URL.Path, err.Error())
				writeJSONError(w, http.StatusBadRequest, err.Error())
			},
		}),
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
			logx.Errorf("msg=%q err=%q", "http_shutdown_error", err.Error())
		}
	}()

	logx.Infof("msg=%q http_addr=%q sqlite_path=%q", "notes_api_listening", httpAddr, sqlitePath)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("http server failed: %v", err)
	}
}

func configureLogLevel() {
	raw := envOrDefault("LOG_LEVEL", "info")
	if err := logx.SetLevelFromString(raw); err != nil {
		logx.SetLevel(logx.LevelInfo)
		logx.Warnf("msg=%q value=%q fallback=%q", "invalid_log_level", raw, "info")
		return
	}
	logx.Infof("msg=%q log_level=%q", "log_level_configured", logx.CurrentLevel().String())
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
		logx.Warnf("msg=%q env=%s value=%q fallback=%d", "invalid_integer_env", name, value, fallback)
		return fallback
	}
	return parsed
}

// writeJSONError keeps error responses consistent across startup/router hooks.
func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}
