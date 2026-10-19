package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"regexp"
	"slices"
	"time"
)

const APIVersion = 1

type Action string

const (
	AccountCreate Action = "account.create"
	SessionIssue  Action = "session.issue"
	PlayerSave    Action = "player.save"
	PlayerDelete  Action = "player.delete"
	TextureUpload Action = "texture.upload"
	CheckIn       Action = "account.check_in"
)

var actions = []Action{AccountCreate, SessionIssue, PlayerSave, PlayerDelete, TextureUpload, CheckIn}

func Actions() []Action { return slices.Clone(actions) }

var (
	ErrDenied = errors.New("plugin policy denied")
	ErrFailed = errors.New("plugin unavailable")
)

// Event 不含凭据、邮箱、HTTP 请求或数据库句柄；Before 只允许拒绝，不能改写核心规则。
type Event struct {
	Action      Action
	AccountID   int64
	AccountUUID string
	PlayerUUID  string
	TextureUUID string
	Name        string
	TokenKind   int16
	TextureKind int16
	Public      int16
	SizeBytes   int64
	At          time.Time
}

type Hooks struct {
	Before func(context.Context, Event) error
	After  func(context.Context, Event) error
}

type Actor struct {
	AccountID int64
	UUID      string
}

// 插件接口固定为所有者专用 GET JSON，避免插件自行遗漏鉴权或覆盖核心路由。
type Endpoint struct {
	Path   string
	Handle func(context.Context, Actor) (any, error)
}

type Instance struct {
	Hooks     Hooks
	Endpoints []Endpoint
}

type Definition struct {
	ID          string
	Version     string
	APIVersion  int
	Description string
	Actions     []Action
	Build       func(json.RawMessage) (Instance, error)
}

type Config struct {
	Enabled int16           `json:"enabled"`
	Options json.RawMessage `json:"options"`
}

type Info struct {
	ID          string   `json:"id"`
	Version     string   `json:"version"`
	APIVersion  int      `json:"apiVersion"`
	Description string   `json:"description"`
	Enabled     int16    `json:"enabled"`
	Actions     []Action `json:"actions"`
	Before      bool     `json:"before"`
	After       bool     `json:"after"`
	Endpoints   []string `json:"endpoints"`
}

type entry struct {
	info Info
	impl Instance
}

// Registry 启动时构造后不再修改；回调必须自行保证并发安全。
type Registry struct {
	entries []entry
	logger  *slog.Logger
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

type hookContext struct{ context.Context }

func (c hookContext) Value(key any) any {
	if _, ok := key.(requestIDKey); ok {
		return c.Context.Value(key)
	}
	return nil
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]{0,47}$`)
var endpointPath = regexp.MustCompile(`^/[a-z][a-z0-9-]{0,47}$`)

func DecodeOptions(raw json.RawMessage, target any) error {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("null options")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return fmt.Errorf("decode plugin options")
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("trailing plugin options")
	}
	return nil
}

// 配置与注册表错误只报告固定标识，不回显可能含密钥的配置内容或插件错误。
func New(definitions []Definition, configs map[string]Config, logger *slog.Logger) (*Registry, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	r := &Registry{logger: logger}
	seen := map[string]bool{}
	for _, d := range definitions {
		if !identifier.MatchString(d.ID) || seen[d.ID] || d.APIVersion != APIVersion || d.Version == "" || d.Build == nil {
			return nil, fmt.Errorf("invalid plugin definition")
		}
		seen[d.ID] = true
		known := map[Action]bool{}
		for _, action := range d.Actions {
			if !slices.Contains(actions, action) || known[action] {
				return nil, fmt.Errorf("invalid plugin actions: %s", d.ID)
			}
			known[action] = true
		}
		c := configs[d.ID]
		if c.Enabled != 0 && c.Enabled != 1 {
			return nil, fmt.Errorf("invalid plugin enabled: %s", d.ID)
		}
		impl, err := build(d.Build, c.Options)
		if err != nil {
			return nil, fmt.Errorf("invalid plugin options: %s", d.ID)
		}
		if len(d.Actions) == 0 && (impl.Hooks.Before != nil || impl.Hooks.After != nil) {
			return nil, fmt.Errorf("missing plugin actions: %s", d.ID)
		}
		paths := []string{}
		for _, ep := range impl.Endpoints {
			if !endpointPath.MatchString(ep.Path) || ep.Handle == nil || slices.Contains(paths, ep.Path) {
				return nil, fmt.Errorf("invalid plugin endpoint: %s", d.ID)
			}
			paths = append(paths, ep.Path)
		}
		impl.Endpoints = slices.Clone(impl.Endpoints)
		info := Info{d.ID, d.Version, d.APIVersion, d.Description, c.Enabled, append([]Action{}, d.Actions...), impl.Hooks.Before != nil, impl.Hooks.After != nil, paths}
		r.entries = append(r.entries, entry{info, impl})
	}
	for id := range configs {
		if !seen[id] {
			return nil, fmt.Errorf("unregistered plugin in configuration")
		}
	}
	return r, nil
}

func build(fn func(json.RawMessage) (Instance, error), raw json.RawMessage) (result Instance, err error) {
	defer func() {
		if recover() != nil {
			err = ErrFailed
		}
	}()
	return fn(raw)
}

func (r *Registry) List() []Info {
	result := []Info{}
	if r != nil {
		for _, e := range r.entries {
			i := e.info
			i.Actions, i.Endpoints = slices.Clone(i.Actions), slices.Clone(i.Endpoints)
			result = append(result, i)
		}
	}
	return result
}

func invoke(ctx context.Context, event Event, fn func(context.Context, Event) error) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrFailed
		}
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	return fn(hookContext{ctx}, event)
}

func (r *Registry) Before(ctx context.Context, event Event) error {
	if r == nil {
		return nil
	}
	for _, e := range r.entries {
		if e.info.Enabled != 1 || e.impl.Hooks.Before == nil || !slices.Contains(e.info.Actions, event.Action) {
			continue
		}
		err := invoke(ctx, event, e.impl.Hooks.Before)
		if err != nil {
			result := ErrFailed
			if errors.Is(err, ErrDenied) {
				result = ErrDenied
			}
			level := slog.LevelError
			if result == ErrDenied {
				level = slog.LevelWarn
			}
			r.logger.Log(ctx, level, "plugin hook rejected", "plugin_id", e.info.ID, "hook", event.Action, "denied", result == ErrDenied, "request_id", RequestID(ctx))
			return result
		}
	}
	return nil
}

// After 只能在事务成功提交后调用；通知失败不把已完成的业务伪装成失败，亦不重放。
func (r *Registry) After(ctx context.Context, event Event) {
	if r == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
	defer cancel()
	for _, e := range r.entries {
		if e.info.Enabled != 1 || e.impl.Hooks.After == nil || !slices.Contains(e.info.Actions, event.Action) {
			continue
		}
		if err := invoke(ctx, event, e.impl.Hooks.After); err != nil {
			r.logger.ErrorContext(ctx, "plugin notification failed", "plugin_id", e.info.ID, "hook", event.Action, "request_id", RequestID(ctx))
		}
	}
}

func (r *Registry) Query(ctx context.Context, id, path string, actor Actor) (value json.RawMessage, err error) {
	defer func() {
		if recover() != nil {
			value, err = nil, ErrFailed
		}
	}()
	if r != nil {
		for _, e := range r.entries {
			if e.info.ID != id || e.info.Enabled != 1 {
				continue
			}
			for _, ep := range e.impl.Endpoints {
				if ep.Path == path {
					v, err := ep.Handle(hookContext{ctx}, actor)
					if err != nil {
						return nil, ErrFailed
					}
					data, err := json.Marshal(v)
					if err != nil {
						return nil, ErrFailed
					}
					return json.RawMessage(data), nil
				}
			}
		}
	}
	return nil, ErrDenied
}
