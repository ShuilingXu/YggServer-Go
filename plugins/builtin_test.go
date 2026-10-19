package plugins_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/ShuilingXu/YggServer-Go/plugin"
	"github.com/ShuilingXu/YggServer-Go/plugins"
)

func TestReservedNamesAndConcurrentActivity(t *testing.T) {
	r, err := plugin.New(plugins.Definitions(), map[string]plugin.Config{
		"reserved-names": {Enabled: 1, Options: json.RawMessage(`{"names":["Admin"]}`)},
		"activity":       {Enabled: 1},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, action := range []plugin.Action{plugin.AccountCreate, plugin.PlayerSave} {
		if err := r.Before(ctx, plugin.Event{Action: action, Name: "aDMiN"}); !errors.Is(err, plugin.ErrDenied) {
			t.Fatalf("%s reserved name accepted", action)
		}
		if err := r.Before(ctx, plugin.Event{Action: action, Name: "Normal"}); err != nil {
			t.Fatalf("normal name rejected: %v", err)
		}
	}
	if err := r.Before(ctx, plugin.Event{Action: plugin.PlayerDelete, Name: "Admin"}); err != nil {
		t.Fatal("existing reserved player cannot be removed")
	}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Go(func() { r.After(ctx, plugin.Event{Action: plugin.CheckIn}) })
	}
	wg.Wait()
	v, err := r.Query(ctx, "activity", "/counts", plugin.Actor{})
	var counts map[plugin.Action]uint64
	if err != nil || json.Unmarshal(v, &counts) != nil || counts[plugin.CheckIn] != 100 {
		t.Fatalf("concurrent committed count = %v, err=%v", v, err)
	}
	for _, raw := range []string{`{"names":["!"]}`, `{"names":["ab"]}`, `{"names":[],"secret":"must-not-echo"}`, `null`} {
		if _, err := plugin.New(plugins.Definitions(), map[string]plugin.Config{"reserved-names": {Options: json.RawMessage(raw)}}, nil); err == nil {
			t.Fatalf("invalid options accepted: %s", raw)
		}
	}
}
