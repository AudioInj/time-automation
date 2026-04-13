// config/config.go
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Domain      string
	Subdomain   string
	Username    string
	Password    string
	WebhookURL  string
	StateFile   string
	ICSCacheDir string

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
	WebPort string // HTTP port for the status UI, e.g. ":8077"

	HolidayAddress  string
	VacationAddress string
	VacationKeyword string
}

// validateWorkDays checks that the WORK_DAYS value is a comma-separated list
// of day numbers (0–6) or ranges (e.g. "1-5", "1,3,5", "0,6").
func validateWorkDays(s string) error {
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, "-") {
			bounds := strings.SplitN(part, "-", 2)
			start, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil || start < 0 || end > 6 || start > end {
				return fmt.Errorf("invalid range %q (use day numbers 0-6, e.g. \"1-5\")", part)
			}
		} else {
			day, err := strconv.Atoi(part)
			if err != nil || day < 0 || day > 6 {
				return fmt.Errorf("invalid day %q (expected a number 0-6)", part)
			}
		}
	}
	return nil
}

// Load reads configuration from environment variables.
// All validation errors are collected and returned together so the operator
// sees every misconfigured variable in a single startup message.
func Load() (*Config, error) {
	var errs []string
	parseOK := map[string]bool{}

	parseTime := func(key string) time.Time {
		val := os.Getenv(key)
		t, err := time.Parse("15:04", val)
		if err != nil {
			errs = append(errs, fmt.Sprintf("  %s: invalid time %q (expected HH:MM)", key, val))
			parseOK[key] = false
		} else {
			parseOK[key] = true
		}
		return t
	}

	parseDuration := func(key string) time.Duration {
		val := os.Getenv(key)
		d, err := time.ParseDuration(val)
		if err != nil {
			errs = append(errs, fmt.Sprintf("  %s: invalid duration %q (e.g. 8.5h)", key, val))
			parseOK[key] = false
		} else {
			parseOK[key] = true
		}
		return d
	}

	stateFile := os.Getenv("STATE_FILE")
	if stateFile == "" {
		stateFile = "/app/state.json"
	}

	icsDir := os.Getenv("ICS_CACHE_DIR")
	if icsDir == "" {
		icsDir = filepath.Dir(stateFile)
	}

	webPort := os.Getenv("WEB_PORT")
	if webPort == "" {
		webPort = ":8077"
	} else if webPort[0] != ':' {
		webPort = ":" + webPort
	}

	cfg := &Config{
		Domain:      os.Getenv("DOMAIN"),
		Subdomain:   os.Getenv("SUBDOMAIN"),
		Username:    os.Getenv("USERNAME"),
		Password:    os.Getenv("PASSWORD"),
		WebhookURL:  os.Getenv("WEBHOOK_URL"),
		StateFile:   stateFile,
		ICSCacheDir: icsDir,

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
		WebPort: webPort,

		HolidayAddress:  os.Getenv("HOLIDAY_ADDRESS"),
		VacationAddress: os.Getenv("VACATION_ADDRESS"),
		VacationKeyword: os.Getenv("VACATION_KEYWORD"),
	}

	// WorkDays format validation
	if cfg.WorkDays != "" {
		if err := validateWorkDays(cfg.WorkDays); err != nil {
			errs = append(errs, "  WORK_DAYS: "+err.Error())
		}
	}

	// Required field validation
	if cfg.Domain == "" {
		errs = append(errs, "  DOMAIN: required")
	}
	if cfg.Subdomain == "" {
		errs = append(errs, "  SUBDOMAIN: required")
	}
	if cfg.Username == "" {
		errs = append(errs, "  USERNAME: required")
	}
	if cfg.Password == "" {
		errs = append(errs, "  PASSWORD: required")
	}

	// Logical constraint validation (only when both values parsed successfully)
	if parseOK["START_WORK_MIN"] && parseOK["START_WORK_MAX"] && cfg.StartWorkMax.Before(cfg.StartWorkMin) {
		errs = append(errs, "  START_WORK_MAX must not be before START_WORK_MIN")
	}
	if parseOK["START_BREAK_MIN"] && parseOK["START_BREAK_MAX"] && cfg.StartBreakMax.Before(cfg.StartBreakMin) {
		errs = append(errs, "  START_BREAK_MAX must not be before START_BREAK_MIN")
	}
	if parseOK["MIN_WORK_DURATION"] && parseOK["MAX_WORK_DURATION"] && cfg.MaxWorkDuration < cfg.MinWorkDuration {
		errs = append(errs, "  MAX_WORK_DURATION must not be less than MIN_WORK_DURATION")
	}
	if parseOK["MIN_BREAK_DURATION"] && parseOK["MAX_BREAK_DURATION"] && cfg.MaxBreakDuration < cfg.MinBreakDuration {
		errs = append(errs, "  MAX_BREAK_DURATION must not be less than MIN_BREAK_DURATION")
	}

	if len(errs) > 0 {
		return nil, fmt.Errorf("configuration errors:\n%s", strings.Join(errs, "\n"))
	}
	return cfg, nil
}
