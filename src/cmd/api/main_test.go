package main

import (
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
