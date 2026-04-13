package config

import (
	"strings"
	"testing"
)

func validEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DOMAIN", "example.com")
	t.Setenv("SUBDOMAIN", "time")
	t.Setenv("USERNAME", "user")
	t.Setenv("PASSWORD", "pass")
	t.Setenv("START_WORK_MIN", "08:00")
	t.Setenv("START_WORK_MAX", "09:00")
	t.Setenv("START_BREAK_MIN", "12:00")
	t.Setenv("START_BREAK_MAX", "13:00")
	t.Setenv("MIN_WORK_DURATION", "8h")
	t.Setenv("MAX_WORK_DURATION", "9h")
	t.Setenv("MIN_BREAK_DURATION", "0.5h")
	t.Setenv("MAX_BREAK_DURATION", "1h")
}

func TestLoadValid(t *testing.T) {
	validEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Domain != "example.com" {
		t.Errorf("Domain = %q, want %q", cfg.Domain, "example.com")
	}
	if cfg.StartWorkMin.Hour() != 8 {
		t.Errorf("StartWorkMin hour = %d, want 8", cfg.StartWorkMin.Hour())
	}
	if cfg.MinWorkDuration.Hours() != 8 {
		t.Errorf("MinWorkDuration = %v, want 8h", cfg.MinWorkDuration)
	}
}

func TestLoadInvalidTime(t *testing.T) {
	validEnv(t)
	t.Setenv("START_WORK_MIN", "not-a-time")
	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid time")
	}
	if !strings.Contains(err.Error(), "START_WORK_MIN") {
		t.Errorf("error should mention START_WORK_MIN, got: %v", err)
	}
}

func TestLoadInvalidDuration(t *testing.T) {
	validEnv(t)
	t.Setenv("MIN_WORK_DURATION", "not-a-duration")
	_, err := Load()
	if err == nil {
		t.Error("expected error for invalid duration")
	}
	if !strings.Contains(err.Error(), "MIN_WORK_DURATION") {
		t.Errorf("error should mention MIN_WORK_DURATION, got: %v", err)
	}
}

func TestLoadCollectsAllErrors(t *testing.T) {
	validEnv(t)
	t.Setenv("START_WORK_MIN", "bad")
	t.Setenv("MIN_WORK_DURATION", "bad")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "START_WORK_MIN") || !strings.Contains(err.Error(), "MIN_WORK_DURATION") {
		t.Errorf("expected both errors reported, got: %v", err)
	}
}

func TestLoadDefaultStateFile(t *testing.T) {
	validEnv(t)
	t.Setenv("STATE_FILE", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateFile != "/app/state.json" {
		t.Errorf("StateFile = %q, want /app/state.json", cfg.StateFile)
	}
}

func TestLoadCustomStateFile(t *testing.T) {
	validEnv(t)
	t.Setenv("STATE_FILE", "/data/state.json")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StateFile != "/data/state.json" {
		t.Errorf("StateFile = %q, want /data/state.json", cfg.StateFile)
	}
}

func TestLoadDryRunAndVerbose(t *testing.T) {
	validEnv(t)
	t.Setenv("DRY_RUN", "true")
	t.Setenv("VERBOSE", "true")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.DryRun {
		t.Error("DryRun should be true")
	}
	if !cfg.Verbose {
		t.Error("Verbose should be true")
	}
}

func TestLoadDefaultICSCacheDir(t *testing.T) {
	validEnv(t)
	t.Setenv("STATE_FILE", "/data/state.json")
	t.Setenv("ICS_CACHE_DIR", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ICSCacheDir != "/data" {
		t.Errorf("ICSCacheDir = %q, want /data", cfg.ICSCacheDir)
	}
}

func TestLoadCustomICSCacheDir(t *testing.T) {
	validEnv(t)
	t.Setenv("ICS_CACHE_DIR", "/tmp/ics")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ICSCacheDir != "/tmp/ics" {
		t.Errorf("ICSCacheDir = %q, want /tmp/ics", cfg.ICSCacheDir)
	}
}

func TestLoadConstraintWorkTimeViolation(t *testing.T) {
	validEnv(t)
	// MAX before MIN
	t.Setenv("START_WORK_MIN", "09:00")
	t.Setenv("START_WORK_MAX", "08:00")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for START_WORK_MAX < START_WORK_MIN")
	}
	if !strings.Contains(err.Error(), "START_WORK_MAX") {
		t.Errorf("error should mention START_WORK_MAX, got: %v", err)
	}
}

func TestLoadConstraintWorkDurationViolation(t *testing.T) {
	validEnv(t)
	t.Setenv("MIN_WORK_DURATION", "9h")
	t.Setenv("MAX_WORK_DURATION", "8h")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for MAX_WORK_DURATION < MIN_WORK_DURATION")
	}
	if !strings.Contains(err.Error(), "MAX_WORK_DURATION") {
		t.Errorf("error should mention MAX_WORK_DURATION, got: %v", err)
	}
}
