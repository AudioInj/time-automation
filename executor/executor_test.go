package executor

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/audioinj/time-automation/config"
	"testing"
)

func TestDryRunSkipsBrowser(t *testing.T) {
	e := New(config.Config{DryRun: true})
	e.run = func(context.Context, []byte) error { t.Fatal("browser called"); return nil }
	if err := e.StartWork(t.Context()); err != nil {
		t.Fatal(err)
	}
}
func TestStableKeyAndErrorPropagation(t *testing.T) {
	cfg := config.Config{Domain: "example.com", Subdomain: "time", Username: "alice", StateFile: "/data/state.json"}
	var keys []string
	for range 2 {
		e := New(cfg)
		e.run = func(_ context.Context, b []byte) error {
			var p map[string]interface{}
			if err := json.Unmarshal(b, &p); err != nil {
				t.Fatal(err)
			}
			keys = append(keys, p["key"].(string))
			return errors.New("unconfirmed")
		}
		if e.StartWork(t.Context()) == nil {
			t.Fatal("failure swallowed")
		}
		if e.StartWork(t.Context()) == nil {
			t.Fatal("cooldown missing")
		}
	}
	if len(keys) != 2 || keys[0] != keys[1] {
		t.Fatal("key changed across executor restart")
	}
}
func TestActionsHaveDifferentKeys(t *testing.T) {
	e := New(config.Config{})
	seen := map[string]bool{}
	e.run = func(_ context.Context, b []byte) error {
		var p map[string]interface{}
		if err := json.Unmarshal(b, &p); err != nil {
			t.Fatal(err)
		}
		key := p["key"].(string)
		if seen[key] {
			t.Fatal("duplicate action key")
		}
		seen[key] = true
		return nil
	}
	for _, f := range []func(context.Context) error{e.StartWork, e.StopWork, e.StartBreak, e.StopBreak} {
		if err := f(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
