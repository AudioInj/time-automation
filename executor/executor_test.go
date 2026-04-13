package executor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/audioinj/time-automation/config"
)

// roundTripFunc allows a plain function to satisfy http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResp(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
	}
}

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
	ctx := context.Background()
	e := New(cfg)
	e.StartWork(ctx)
	e.StopWork(ctx)
	e.StartBreak(ctx)
	e.StopBreak(ctx)
}

func TestVerboseLogEnabled(t *testing.T) {
	e := New(config.Config{Verbose: true})
	e.VerboseLog("verbose enabled – should not panic")
}

func TestVerboseLogDisabled(t *testing.T) {
	e := New(config.Config{Verbose: false})
	e.VerboseLog("verbose disabled – should not panic")
}

// ---------------------------------------------------------------------------
// Login and retry logic
// ---------------------------------------------------------------------------

func TestLoginTokenCached(t *testing.T) {
	loginCalls := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/api/login") {
			loginCalls++
			return jsonResp(200, `{"token":"tok-abc"}`), nil
		}
		return jsonResp(200, `{}`), nil
	})

	cfg := config.Config{
		Domain: "example.com", Subdomain: "time",
		Username: "u", Password: "p", Task: "work",
	}
	ctx := context.Background()
	e := New(cfg)
	e.client = &http.Client{Transport: transport}
	e.retrySleep = 0

	e.StartWork(ctx) // login once, then post
	e.StopWork(ctx)  // token already cached: no second login

	if loginCalls != 1 {
		t.Errorf("expected 1 login call, got %d", loginCalls)
	}
}

func TestPost401TriggersReauth(t *testing.T) {
	loginCalls := 0
	postAttempts := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/api/login") {
			loginCalls++
			return jsonResp(200, `{"token":"fresh-token"}`), nil
		}
		postAttempts++
		if postAttempts == 1 {
			return jsonResp(401, ``), nil // first post → 401
		}
		return jsonResp(200, `{}`), nil // retry → success
	})

	cfg := config.Config{
		Domain: "example.com", Subdomain: "time",
		Username: "u", Password: "p", Task: "work",
	}
	ctx := context.Background()
	e := New(cfg)
	e.client = &http.Client{Transport: transport}
	e.retrySleep = 0

	e.StartWork(ctx)

	if loginCalls != 2 {
		t.Errorf("expected 2 login calls (initial + re-auth on 401), got %d", loginCalls)
	}
	if postAttempts != 2 {
		t.Errorf("expected 2 post attempts, got %d", postAttempts)
	}
}

func TestPostRetryExhaustion(t *testing.T) {
	postAttempts := 0
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.Contains(r.URL.Path, "/api/login") {
			return jsonResp(200, `{"token":"tok"}`), nil
		}
		postAttempts++
		return jsonResp(503, ``), nil // always fail
	})

	cfg := config.Config{
		Domain: "example.com", Subdomain: "time",
		Username: "u", Password: "p", Task: "work",
	}
	ctx := context.Background()
	e := New(cfg)
	e.client = &http.Client{Transport: transport}
	e.retrySleep = 0

	e.StartWork(ctx) // should exhaust all 5 retries

	if postAttempts != 5 {
		t.Errorf("expected 5 post attempts, got %d", postAttempts)
	}
}
