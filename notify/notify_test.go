package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendEmptyWebhook(t *testing.T) {
	New("").Send(context.Background(), "status", "message") // must not panic
}

func TestSendReachesWebhook(t *testing.T) {
	called := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	New(ts.URL).Send(context.Background(), "Test", "Hello")
	if !called {
		t.Error("expected webhook server to receive a request")
	}
}

func TestSendPayloadShape(t *testing.T) {
	var payload map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("failed to unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	New(ts.URL).Send(context.Background(), "MyTitle", "MyMessage")

	embeds, ok := payload["embeds"].([]interface{})
	if !ok || len(embeds) != 1 {
		t.Fatalf("expected 1 embed, got: %v", payload["embeds"])
	}
	embed, ok := embeds[0].(map[string]interface{})
	if !ok {
		t.Fatal("embed is not an object")
	}
	if embed["title"] != "MyTitle" {
		t.Errorf("title = %q, want %q", embed["title"], "MyTitle")
	}
	if embed["description"] != "MyMessage" {
		t.Errorf("description = %q, want %q", embed["description"], "MyMessage")
	}
}

func TestSendContentType(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct := r.Header.Get("Content-Type")
		if ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	New(ts.URL).Send(context.Background(), "title", "body")
}

func TestSendLogsOnErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	// Should not panic; error is only logged
	New(ts.URL).Send(context.Background(), "title", "body")
}
