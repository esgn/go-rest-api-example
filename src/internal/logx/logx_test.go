package logx

import (
	"bytes"
	"log"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in      string
		want    Level
		wantErr bool
	}{
		{in: "debug", want: LevelDebug},
		{in: "INFO", want: LevelInfo},
		{in: "warn", want: LevelWarn},
		{in: "warning", want: LevelWarn},
		{in: "error", want: LevelError},
		{in: "", want: LevelInfo},
		{in: "bad", wantErr: true},
	}

	for _, tc := range tests {
		got, err := ParseLevel(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("ParseLevel(%q): expected error", tc.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseLevel(%q): %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestLevelFiltering(t *testing.T) {
	prev := CurrentLevel()
	defer SetLevel(prev)

	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(orig)

	SetLevel(LevelWarn)
	Infof("msg=%q", "info_message")
	Warnf("msg=%q", "warn_message")

	out := buf.String()
	if strings.Contains(out, "info_message") {
		t.Fatal("info log should be filtered out at warn level")
	}
	if !strings.Contains(out, "warn_message") {
		t.Fatal("warn log should be emitted at warn level")
	}
	if !strings.Contains(out, "level=WARN") {
		t.Fatal("warn log should include WARN level prefix")
	}
}
