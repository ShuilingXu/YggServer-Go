package plugin_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/ShuilingXu/YggServer-Go/plugin"
)

func TestHookPolicyFailureAndCommitNotification(t *testing.T) {
	for _, tc := range []struct {
		name        string
		failure     error
		panicBefore bool
		want        error
	}{
		{"deny", plugin.ErrDenied, false, plugin.ErrDenied},
		{"failure", errors.New("secret-from-plugin"), false, plugin.ErrFailed},
		{"panic", nil, true, plugin.ErrFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			calls := 0
			defs := []plugin.Definition{
				{ID: "first", Version: "1", APIVersion: 1, Actions: []plugin.Action{plugin.PlayerSave}, Build: func(json.RawMessage) (plugin.Instance, error) {
					return plugin.Instance{Hooks: plugin.Hooks{Before: func(context.Context, plugin.Event) error {
						if tc.panicBefore {
							panic("secret-panic")
						}
						return tc.failure
					}, After: func(context.Context, plugin.Event) error { panic("secret-notification") }}}, nil
				}},
				{ID: "second", Version: "1", APIVersion: 1, Actions: []plugin.Action{plugin.PlayerSave}, Build: func(json.RawMessage) (plugin.Instance, error) {
					return plugin.Instance{Hooks: plugin.Hooks{Before: func(context.Context, plugin.Event) error { calls++; return nil }, After: func(context.Context, plugin.Event) error { calls++; return nil }}}, nil
				}},
			}
			r, err := plugin.New(defs, map[string]plugin.Config{"first": {Enabled: 1}, "second": {Enabled: 1}}, slog.New(slog.NewJSONHandler(&logs, nil)))
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Before(context.Background(), plugin.Event{Action: plugin.PlayerSave}); !errors.Is(err, tc.want) || calls != 0 {
				t.Fatalf("rejection = %v, subsequent calls = %d", err, calls)
			}
			// 提交后的通知即使一个插件 panic，也必须继续通知后面的插件。
			r.After(context.Background(), plugin.Event{Action: plugin.PlayerSave})
			if calls != 1 {
				t.Fatalf("subsequent observer calls = %d, want 1", calls)
			}
			if strings.Contains(logs.String(), "secret") {
				t.Fatal("plugin error or panic leaked")
			}
		})
	}
}

func TestPluginRegistrationRejectsUnsafeDefinitions(t *testing.T) {
	valid := plugin.Definition{ID: "example", Version: "1", APIVersion: 1, Build: func(json.RawMessage) (plugin.Instance, error) { return plugin.Instance{}, nil }}
	for _, tc := range []struct {
		name   string
		defs   []plugin.Definition
		config map[string]plugin.Config
	}{
		{"duplicate", []plugin.Definition{valid, valid}, nil},
		{"unknown-config", []plugin.Definition{valid}, map[string]plugin.Config{"missing": {Enabled: 1}}},
		{"invalid-toggle", []plugin.Definition{valid}, map[string]plugin.Config{"example": {Enabled: 2}}},
		{"api-version", []plugin.Definition{{ID: "example", Version: "1", APIVersion: 2, Build: valid.Build}}, nil},
		{"unknown-hook", []plugin.Definition{{ID: "example", Version: "1", APIVersion: 1, Actions: []plugin.Action{"bad"}, Build: valid.Build}}, nil},
		{"route-escape", []plugin.Definition{{ID: "example", Version: "1", APIVersion: 1, Build: func(json.RawMessage) (plugin.Instance, error) {
			return plugin.Instance{Endpoints: []plugin.Endpoint{{Path: "/../admin", Handle: func(context.Context, plugin.Actor) (any, error) { return nil, nil }}}}, nil
		}}}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := plugin.New(tc.defs, tc.config, nil); err == nil {
				t.Fatal("unsafe registration accepted")
			}
		})
	}
}

func TestDisabledPluginAndContextCredentialIsolation(t *testing.T) {
	type secretKey struct{}
	ctx := context.WithValue(context.Background(), secretKey{}, "secret")
	calls := 0
	d := plugin.Definition{ID: "example", Version: "1", APIVersion: 1, Actions: []plugin.Action{plugin.CheckIn}, Build: func(json.RawMessage) (plugin.Instance, error) {
		return plugin.Instance{Hooks: plugin.Hooks{Before: func(ctx context.Context, e plugin.Event) error {
			if ctx.Value(secretKey{}) != nil {
				t.Fatal("credential context leaked")
			}
			calls++
			return nil
		}}, Endpoints: []plugin.Endpoint{{Path: "/status", Handle: func(ctx context.Context, a plugin.Actor) (any, error) {
			if ctx.Value(secretKey{}) != nil {
				t.Fatal("endpoint context leaked")
			}
			return a.UUID, nil
		}}}}, nil
	}}
	r, err := plugin.New([]plugin.Definition{d}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Before(ctx, plugin.Event{Action: plugin.CheckIn}); err != nil || calls != 0 {
		t.Fatal("disabled hook executed")
	}
	if _, err := r.Query(ctx, "example", "/status", plugin.Actor{}); !errors.Is(err, plugin.ErrDenied) {
		t.Fatal("disabled endpoint accepted")
	}
	r, err = plugin.New([]plugin.Definition{d}, map[string]plugin.Config{"example": {Enabled: 1}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Before(ctx, plugin.Event{Action: plugin.CheckIn}); err != nil || calls != 1 {
		t.Fatal("enabled hook did not execute")
	}
	if v, err := r.Query(ctx, "example", "/status", plugin.Actor{UUID: "account"}); err != nil || string(v) != `"account"` {
		t.Fatal("endpoint failed")
	}
}

func TestEndpointFailureDoesNotEscape(t *testing.T) {
	for _, panicHandler := range []bool{false, true} {
		d := plugin.Definition{ID: "example", Version: "1", APIVersion: 1, Build: func(json.RawMessage) (plugin.Instance, error) {
			return plugin.Instance{Endpoints: []plugin.Endpoint{{Path: "/status", Handle: func(context.Context, plugin.Actor) (any, error) {
				if panicHandler {
					panic("secret")
				}
				return make(chan int), nil
			}}}}, nil
		}}
		r, err := plugin.New([]plugin.Definition{d}, map[string]plugin.Config{"example": {Enabled: 1}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := r.Query(context.Background(), "example", "/status", plugin.Actor{}); !errors.Is(err, plugin.ErrFailed) {
			t.Fatal("invalid endpoint response accepted")
		}
	}
}
