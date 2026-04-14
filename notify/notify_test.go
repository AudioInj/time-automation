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

func TestSendSlackPayloadShape(t *testing.T) {
	var payload map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("failed to unmarshal request body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	// Embed "hooks.slack.com" in the path so the URL-based detection triggers
	slackURL := ts.URL + "/hooks.slack.com/services/T000/B000/xxxx"
	New(slackURL).Send(context.Background(), "▶️ Arbeit gestartet", "08:14:23")

	text, ok := payload["text"].(string)
	if !ok {
		t.Fatalf("expected 'text' field in Slack payload, got: %v", payload)
	}
	if _, hasEmbeds := payload["embeds"]; hasEmbeds {
		t.Error("Slack payload must not contain 'embeds'")
	}
	if text != "*▶️ Arbeit gestartet*\n08:14:23" {
		t.Errorf("unexpected Slack text: %q", text)
	}
}

func TestSendSlackEmptyMessage(t *testing.T) {
	var payload map[string]interface{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &payload)
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	slackURL := ts.URL + "/hooks.slack.com/services/T000/B000/xxxx"
	New(slackURL).Send(context.Background(), "Kein Arbeitstag", "")

	text, _ := payload["text"].(string)
	if text != "*Kein Arbeitstag*" {
		t.Errorf("expected status only without trailing newline, got: %q", text)
	}
}
