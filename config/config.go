// config/config.go
package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

type Config struct {
	Domain     string
	Subdomain  string
	Username   string
	Password   string
	WebhookURL string
	StateFile  string

	StartWorkMin  time.Time
	StartWorkMax  time.Time
	StartBreakMin time.Time
	StartBreakMax time.Time

	MinWorkDuration  time.Duration
	MaxWorkDuration  time.Duration
	MinBreakDuration time.Duration
	MaxBreakDuration time.Duration

	WorkDays string // e.g. "1-5"

	Task    string
	Verbose bool
	DryRun  bool

	HolidayAddress  string
	VacationAddress string
	VacationKeyword string
}

// Load reads configuration from environment variables.
// All validation errors are collected and returned together so the operator
// sees every misconfigured variable in a single startup message.
func Load() (*Config, error) {
	var errs []string

	parseTime := func(key string) time.Time {
		val := os.Getenv(key)
		t, err := time.Parse("15:04", val)
		if err != nil {
			errs = append(errs, fmt.Sprintf("  %s: invalid time %q (expected HH:MM)", key, val))
		}
		return t
	}

	parseDuration := func(key string) time.Duration {
		val := os.Getenv(key)
		d, err := time.ParseDuration(val)
		if err != nil {
			errs = append(errs, fmt.Sprintf("  %s: invalid duration %q (e.g. 8.5h)", key, val))
		}
		return d
	}

	stateFile := os.Getenv("STATE_FILE")
	if stateFile == "" {
		stateFile = "/app/state.json"
	}

	cfg := &Config{
		Domain:     os.Getenv("DOMAIN"),
		Subdomain:  os.Getenv("SUBDOMAIN"),
		Username:   os.Getenv("USERNAME"),
		Password:   os.Getenv("PASSWORD"),
		WebhookURL: os.Getenv("WEBHOOK_URL"),
		StateFile:  stateFile,

		StartWorkMin:  parseTime("START_WORK_MIN"),
		StartWorkMax:  parseTime("START_WORK_MAX"),
		StartBreakMin: parseTime("START_BREAK_MIN"),
		StartBreakMax: parseTime("START_BREAK_MAX"),

		MinWorkDuration:  parseDuration("MIN_WORK_DURATION"),
		MaxWorkDuration:  parseDuration("MAX_WORK_DURATION"),
		MinBreakDuration: parseDuration("MIN_BREAK_DURATION"),
		MaxBreakDuration: parseDuration("MAX_BREAK_DURATION"),

		WorkDays: os.Getenv("WORK_DAYS"),

		Task:    os.Getenv("TASK"),
		Verbose: os.Getenv("VERBOSE") == "true",
		DryRun:  os.Getenv("DRY_RUN") == "true",

		HolidayAddress:  os.Getenv("HOLIDAY_ADDRESS"),
		VacationAddress: os.Getenv("VACATION_ADDRESS"),
		VacationKeyword: os.Getenv("VACATION_KEYWORD"),
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration errors:\n%s", strings.Join(errs, "\n"))
	}
	return cfg, nil
}
