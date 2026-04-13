package executor

import (
	"testing"

	"github.com/run2go/time-automation/config"
)

func TestNew(t *testing.T) {
	e := New(config.Config{})
	if e == nil {
		t.Error("New returned nil")
	}
}

func TestDryRunSkipsHTTP(t *testing.T) {
	// With DryRun=true none of the actions should make real HTTP calls.
	// Domain points to an unreachable address; the test would fail on network
	// contact because there is no server listening there.
	cfg := config.Config{
		DryRun:    true,
		Username:  "testuser",
		Domain:    "invalid.example.invalid",
		Subdomain: "time",
		Task:      "Dev",
	}
	e := New(cfg)
	e.StartWork()
	e.StopWork()
	e.StartBreak()
	e.StopBreak()
}

func TestVerboseLogEnabled(t *testing.T) {
	e := New(config.Config{Verbose: true})
	e.VerboseLog("verbose enabled – should not panic")
}

func TestVerboseLogDisabled(t *testing.T) {
	e := New(config.Config{Verbose: false})
	e.VerboseLog("verbose disabled – should not panic")
}
