package main

import (
	"log/slog"
	"testing"
)

// ── envOrDefault ─────────────────────────────────────────────────────────────

func TestEnvOrDefault_Set(t *testing.T) {
	t.Setenv("TEST_ENV_OR_DEFAULT", "custom")
	got := envOrDefault("TEST_ENV_OR_DEFAULT", "fallback")
	if got != "custom" {
		t.Errorf("got %q, want %q", got, "custom")
	}
}

func TestEnvOrDefault_Unset(t *testing.T) {
	// t.Setenv not called → variable is absent
	got := envOrDefault("TEST_ENV_ABSENT_VAR_12345", "fallback")
	if got != "fallback" {
		t.Errorf("got %q, want %q", got, "fallback")
	}
}

func TestEnvOrDefault_Empty(t *testing.T) {
	t.Setenv("TEST_ENV_OR_DEFAULT_EMPTY", "")
	got := envOrDefault("TEST_ENV_OR_DEFAULT_EMPTY", "fallback")
	if got != "fallback" {
		t.Errorf("got %q, want %q (empty string should use fallback)", got, "fallback")
	}
}

// ── envOrDefaultInt ──────────────────────────────────────────────────────────

func TestEnvOrDefaultInt_ValidInt(t *testing.T) {
	t.Setenv("TEST_ENV_INT", "42")
	got := envOrDefaultInt("TEST_ENV_INT", 10)
	if got != 42 {
		t.Errorf("got %d, want 42", got)
	}
}

func TestEnvOrDefaultInt_Unset(t *testing.T) {
	got := envOrDefaultInt("TEST_ENV_INT_ABSENT_12345", 10)
	if got != 10 {
		t.Errorf("got %d, want 10 (fallback)", got)
	}
}

func TestEnvOrDefaultInt_InvalidInt(t *testing.T) {
	t.Setenv("TEST_ENV_INT_BAD", "not_a_number")
	got := envOrDefaultInt("TEST_ENV_INT_BAD", 10)
	if got != 10 {
		t.Errorf("got %d, want 10 (fallback for invalid int)", got)
	}
}

func TestEnvOrDefaultInt_Empty(t *testing.T) {
	t.Setenv("TEST_ENV_INT_EMPTY", "")
	got := envOrDefaultInt("TEST_ENV_INT_EMPTY", 10)
	if got != 10 {
		t.Errorf("got %d, want 10 (fallback for empty string)", got)
	}
}

func TestEnvOrDefaultInt_Negative(t *testing.T) {
	t.Setenv("TEST_ENV_INT_NEG", "-5")
	got := envOrDefaultInt("TEST_ENV_INT_NEG", 10)
	if got != -5 {
		t.Errorf("got %d, want -5", got)
	}
}

func TestEnvOrDefaultInt_Zero(t *testing.T) {
	t.Setenv("TEST_ENV_INT_ZERO", "0")
	got := envOrDefaultInt("TEST_ENV_INT_ZERO", 10)
	if got != 0 {
		t.Errorf("got %d, want 0", got)
	}
}

// ── loadServiceConfigFromEnv ────────────────────────────────────────────────

func TestLoadServiceConfigFromEnv_DefaultsAreValid(t *testing.T) {
	t.Setenv("NOTE_MAX_CONTENT_LENGTH", "")
	t.Setenv("NOTE_MAX_TITLE_LENGTH", "")
	t.Setenv("PAGE_DEFAULT_LIMIT", "")
	t.Setenv("PAGE_MAX_LIMIT", "")

	_, err := loadServiceConfigFromEnv()
	if err != nil {
		t.Fatalf("expected default config to be valid, got %v", err)
	}
}

func TestLoadServiceConfigFromEnv_InvalidConfig(t *testing.T) {
	t.Setenv("NOTE_MAX_TITLE_LENGTH", "-1")
	t.Setenv("NOTE_MAX_CONTENT_LENGTH", "100")
	t.Setenv("PAGE_DEFAULT_LIMIT", "20")
	t.Setenv("PAGE_MAX_LIMIT", "100")

	_, err := loadServiceConfigFromEnv()
	if err == nil {
		t.Fatal("expected invalid service config error, got nil")
	}
}

func TestConfigureLogLevel_DefaultsToInfo(t *testing.T) {
	prev := appLogLevel.Level()
	defer appLogLevel.Set(prev)

	t.Setenv("LOG_LEVEL", "")
	configureLogLevel()

	if got := appLogLevel.Level(); got != slog.LevelInfo {
		t.Fatalf("level = %v, want %v", got, slog.LevelInfo)
	}
}

func TestConfigureLogLevel_ValidValue(t *testing.T) {
	prev := appLogLevel.Level()
	defer appLogLevel.Set(prev)

	t.Setenv("LOG_LEVEL", "warn")
	configureLogLevel()

	if got := appLogLevel.Level(); got != slog.LevelWarn {
		t.Fatalf("level = %v, want %v", got, slog.LevelWarn)
	}
}

func TestConfigureLogLevel_InvalidFallsBackToInfo(t *testing.T) {
	prev := appLogLevel.Level()
	defer appLogLevel.Set(prev)

	t.Setenv("LOG_LEVEL", "nope")
	configureLogLevel()

	if got := appLogLevel.Level(); got != slog.LevelInfo {
		t.Fatalf("level = %v, want %v", got, slog.LevelInfo)
	}
}
