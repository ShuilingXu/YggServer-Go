package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/ShuilingXu/YggServer-Go/internal/identity"
	"github.com/ShuilingXu/YggServer-Go/plugin"
)

func Definitions() []plugin.Definition {
	return []plugin.Definition{
		{ID: "reserved-names", Version: "1.0.0", APIVersion: plugin.APIVersion, Description: "阻止新建或改名为保留角色名；不影响旧角色登录与删除", Actions: []plugin.Action{plugin.AccountCreate, plugin.PlayerSave}, Build: reservedNames},
		{ID: "activity", Version: "1.0.0", APIVersion: plugin.APIVersion, Description: "统计本进程已提交的业务事件，重启清零", Actions: plugin.Actions(), Build: activity},
	}
}

func reservedNames(raw json.RawMessage) (plugin.Instance, error) {
	var options struct {
		Names []string `json:"names"`
	}
	if err := plugin.DecodeOptions(raw, &options); err != nil {
		return plugin.Instance{}, err
	}
	if len(options.Names) > 256 {
		return plugin.Instance{}, fmt.Errorf("too many reserved names")
	}
	names := map[string]bool{}
	for _, name := range options.Names {
		if !identity.ValidPlayer(name) {
			return plugin.Instance{}, fmt.Errorf("invalid reserved name")
		}
		names[strings.ToLower(name)] = true
	}
	return plugin.Instance{Hooks: plugin.Hooks{Before: func(ctx context.Context, e plugin.Event) error {
		if names[strings.ToLower(e.Name)] {
			return plugin.ErrDenied
		}
		return nil
	}}}, nil
}

func activity(raw json.RawMessage) (plugin.Instance, error) {
	if err := plugin.DecodeOptions(raw, &struct{}{}); err != nil {
		return plugin.Instance{}, err
	}
	actions := plugin.Actions()
	counts := make([]atomic.Uint64, len(actions))
	return plugin.Instance{
		Hooks: plugin.Hooks{After: func(ctx context.Context, e plugin.Event) error {
			for i, action := range actions {
				if action == e.Action {
					counts[i].Add(1)
					break
				}
			}
			return nil
		}},
		Endpoints: []plugin.Endpoint{{Path: "/counts", Handle: func(ctx context.Context, actor plugin.Actor) (any, error) {
			values := map[plugin.Action]uint64{}
			for i, action := range actions {
				values[action] = counts[i].Load()
			}
			return values, nil
		}}},
	}, nil
}
