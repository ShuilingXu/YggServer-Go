package provision

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/httpapi"
	"github.com/ShuilingXu/YggServer-Go/internal/identity"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

type RunState int16

const (
	RunRunning  RunState = 0
	RunSuccess  RunState = 1
	RunFailed   RunState = 2
	RunCanceled RunState = 3
)

type Run struct {
	ID          string     `json:"id"`
	DryRun      bool       `json:"dryRun"`
	State       RunState   `json:"state"`
	Stage       string     `json:"stage"`
	Processed   int        `json:"processed"`
	Report      Report     `json:"report"`
	CreatedAt   time.Time  `json:"createdAt"`
	CompletedAt *time.Time `json:"completedAt"`
}
type Manager struct {
	Store       *Store
	Accounts    *identity.Handler
	Logger      *slog.Logger
	CodeFile    string
	codeHash    string
	mu          sync.Mutex
	gate        sync.RWMutex
	busy        atomic.Bool
	needsSetup  atomic.Bool
	run         Run
	runKeyHash  string
	previewHash string
	cancel      context.CancelFunc
	wg          sync.WaitGroup
	stopped     bool
}

func New(engine *Store, accounts *identity.Handler, logger *slog.Logger, codeFile string) (*Manager, error) {
	m := &Manager{Store: engine, Accounts: accounts, Logger: logger, CodeFile: codeFile}
	// 运行记录在目标事务外持久化；进程异常退出后显式标记失败。
	if err := engine.recoverRuns(context.Background()); err != nil {
		return nil, fmt.Errorf("import history unavailable")
	}
	empty, err := identity.Uninitialized(context.Background(), engine.DB)
	if err != nil {
		return nil, err
	}
	m.needsSetup.Store(empty)
	if empty && codeFile != "" {
		if err = os.MkdirAll(filepath.Dir(codeFile), 0700); err != nil {
			return nil, fmt.Errorf("setup code directory unavailable")
		}
		f, err := os.OpenFile(codeFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			code := rand.Text()
			_, writeErr := f.WriteString(code)
			closeErr := f.Close()
			if writeErr != nil || closeErr != nil {
				return nil, fmt.Errorf("setup code creation failed")
			}
		} else if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("setup code unavailable")
		}
		info, err := os.Lstat(codeFile)
		if err != nil || !info.Mode().IsRegular() || info.Size() > 128 {
			return nil, fmt.Errorf("invalid setup code file")
		}
		data, err := os.ReadFile(codeFile)
		if err != nil || len(strings.TrimSpace(string(data))) < 20 {
			return nil, fmt.Errorf("invalid setup code file")
		}
		m.codeHash = identity.Hash(strings.TrimSpace(string(data)))
		logger.Info("site setup required", "code_file", codeFile)
	}
	return m, nil
}
func (m *Manager) Close() error {
	m.mu.Lock()
	m.stopped = true
	if m.cancel != nil {
		m.cancel()
	}
	m.mu.Unlock()
	m.wg.Wait()
	return nil
}
func (m *Manager) codeOK(code string) bool {
	return m.codeHash != "" && subtle.ConstantTimeCompare([]byte(m.codeHash), []byte(identity.Hash(code))) == 1
}
func (m *Manager) removeCode() {
	if m.CodeFile != "" {
		if err := os.Remove(m.CodeFile); err != nil && !errors.Is(err, os.ErrNotExist) {
			m.Logger.Warn("setup code cleanup failed")
		}
	}
}
func (m *Manager) setupAllowed(w http.ResponseWriter, r *http.Request, code string) bool {
	empty, err := identity.Uninitialized(r.Context(), m.Store.DB)
	if err != nil {
		httpapi.Error(w, 503)
		return false
	}
	if !empty {
		httpapi.Error(w, 409)
		return false
	}
	if !m.codeOK(code) {
		httpapi.Error(w, 403)
		return false
	}
	return true
}
func (m *Manager) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/setup", func(w http.ResponseWriter, r *http.Request) {
		empty, err := identity.Uninitialized(r.Context(), m.Store.DB)
		if err != nil {
			httpapi.Error(w, 503)
			return
		}
		httpapi.JSON(w, 200, struct {
			Required  bool `json:"required"`
			Available bool `json:"available"`
			Running   bool `json:"running"`
		}{empty, m.codeHash != "", m.busy.Load()})
	})
	mux.HandleFunc("POST /api/setup/verify", m.Accounts.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Code string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		if m.setupAllowed(w, r, body.Code) {
			w.WriteHeader(204)
		}
	}))
	mux.HandleFunc("POST /api/setup", m.Accounts.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Code, Name, Email, Password, Player string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		if !m.setupAllowed(w, r, body.Code) {
			return
		}
		if !m.busy.CompareAndSwap(false, true) {
			httpapi.Error(w, 409)
			return
		}
		defer m.busy.Store(false)
		m.gate.Lock()
		defer m.gate.Unlock()
		err := m.Accounts.Service.Initialize(r.Context(), body.Name, body.Email, body.Password, body.Player, w.Header().Get("X-Request-ID"))
		if err != nil {
			status := 500
			if errors.Is(err, identity.ErrInvalid) {
				status = 400
			}
			if errors.Is(err, identity.ErrConflict) {
				status = 409
			}
			httpapi.Error(w, status)
			return
		}
		m.removeCode()
		m.needsSetup.Store(false)
		m.Logger.Info("site initialized", "request_id", w.Header().Get("X-Request-ID"))
		w.WriteHeader(201)
	}))
	for _, entry := range []string{"setup", "admin"} {
		action := func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Code      string `json:"code"`
				Source    Source `json:"source"`
				DryRun    bool   `json:"dryRun"`
				Confirm   bool   `json:"confirm"`
				PreviewID string `json:"previewId"`
			}
			if !httpapi.Decode(w, r, &body) {
				return
			}
			actor := ""
			if entry == "setup" {
				if !m.setupAllowed(w, r, body.Code) {
					return
				}
			} else {
				a := identity.Current(r.Context()).Account
				if a.Permission != model.PermissionOwner {
					httpapi.Error(w, 403)
					return
				}
				actor = a.UUID
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			// 真正执行必须使用同一份配置、同一进程内成功的试跑，凭据不写入报告。
			encoded, _ := json.Marshal(body.Source)
			fingerprint := identity.Hash(string(encoded))
			if !body.DryRun && (!body.Confirm || body.PreviewID != m.run.ID || m.run.State != RunSuccess || !m.run.DryRun || m.previewHash != fingerprint) {
				httpapi.Error(w, 409)
				return
			}
			if m.stopped || !m.busy.CompareAndSwap(false, true) {
				httpapi.Error(w, 409)
				return
			}
			id := model.UUID()
			key := rand.Text()
			m.run = Run{ID: id, DryRun: body.DryRun, State: RunRunning, Stage: "queued", CreatedAt: m.Store.Now().UTC(), Report: Report{Counts: map[string]int{}, Skipped: map[string]int{}, Issues: []Issue{}, Players: []PlayerMapping{}, Omitted: []string{}}}
			m.runKeyHash = identity.Hash(key)
			m.previewHash = fingerprint
			err := m.Store.recordRun(r.Context(), m.run, true)
			if err != nil {
				m.busy.Store(false)
				httpapi.Error(w, 503)
				return
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			m.cancel = cancel
			m.wg.Add(1)
			requestID := w.Header().Get("X-Request-ID")
			go m.execute(ctx, cancel, body.Source, body.DryRun, actor, requestID)
			m.Logger.Info("import started", "run_id", id, "dry_run", body.DryRun, "request_id", requestID)
			httpapi.JSON(w, 202, struct{ ID, RunKey string }{id, key})
		}
		if entry == "setup" {
			mux.HandleFunc("POST /api/setup/import", m.Accounts.RateLimit(action))
		} else {
			mux.HandleFunc("POST /api/admin/import", m.Accounts.Authorized(model.TokenWeb, action))
		}
	}
	for _, operation := range []string{"status", "cancel"} {
		mux.HandleFunc("POST /api/import/"+operation, func(w http.ResponseWriter, r *http.Request) {
			var body struct{ ID, RunKey string }
			if !httpapi.Decode(w, r, &body) {
				return
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			if body.ID != m.run.ID || m.runKeyHash == "" || subtle.ConstantTimeCompare([]byte(m.runKeyHash), []byte(identity.Hash(body.RunKey))) != 1 {
				httpapi.Error(w, 403)
				return
			}
			if operation == "cancel" && m.run.State == RunRunning && m.cancel != nil {
				m.cancel()
			}
			httpapi.JSON(w, 200, m.run)
		})
	}
	mux.HandleFunc("GET /api/admin/import/history", m.Accounts.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		if identity.Current(r.Context()).Account.Permission != model.PermissionOwner {
			httpapi.Error(w, 403)
			return
		}
		runs, err := m.Store.history(r.Context())
		if err != nil {
			httpapi.Error(w, 503)
			return
		}
		httpapi.JSON(w, 200, runs)
	}))
}
func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func (m *Manager) execute(ctx context.Context, cancel context.CancelFunc, source Source, dry bool, actor, requestID string) {
	defer m.wg.Done()
	defer cancel()
	defer m.busy.Store(false)
	m.gate.Lock()
	defer m.gate.Unlock()
	report, err := m.Store.Run(ctx, source, dry, actor, requestID, func(stage string, processed int) {
		m.mu.Lock()
		m.run.Stage = stage
		m.run.Processed = processed
		m.mu.Unlock()
	})
	m.mu.Lock()
	defer m.mu.Unlock()
	m.run.Report = report
	m.run.State = RunSuccess
	m.run.Stage = "complete"
	if err != nil {
		m.run.State = RunFailed
		m.run.Stage = "failed"
		if errors.Is(ctx.Err(), context.Canceled) {
			m.run.State = RunCanceled
			m.run.Stage = "canceled"
		}
		if report.Blocking == 0 && m.run.State != RunCanceled {
			m.run.Report.issue("run", "", "connection_schema_storage_or_data_error", false)
		}
	}
	now := m.Store.Now().UTC()
	m.run.CompletedAt = &now
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer saveCancel()
	saveErr := m.Store.recordRun(saveCtx, m.run, false)
	if saveErr != nil {
		m.Logger.Error("import report persistence failed", "run_id", m.run.ID)
	}
	level := slog.LevelInfo
	if m.run.State == RunFailed {
		level = slog.LevelWarn
	}
	m.Logger.Log(ctx, level, "import finished", "run_id", m.run.ID, "dry_run", dry, "state", m.run.State, "blocking", report.Blocking, "request_id", requestID)
	if err == nil && !dry {
		m.needsSetup.Store(false)
		m.removeCode()
	}
}

// 所有业务请求持读锁；任务先设维护标志再等在途请求结束，避免导入和上传并发。
func (m *Manager) Guard(w http.ResponseWriter, r *http.Request, next http.Handler) {
	if strings.HasPrefix(r.URL.Path, "/api/setup") || strings.HasPrefix(r.URL.Path, "/api/import/") || r.URL.Path == "/api/admin/import" || strings.HasPrefix(r.URL.Path, "/api/admin/import/") || strings.HasPrefix(r.URL.Path, "/web/") || r.URL.Path == "/healthz" {
		next.ServeHTTP(w, r)
		return
	}
	if m.busy.Load() {
		w.Header().Set("Retry-After", "5")
		httpapi.Error(w, 503)
		return
	}
	m.gate.RLock()
	defer m.gate.RUnlock()
	if m.busy.Load() {
		httpapi.Error(w, 503)
		return
	}
	if m.needsSetup.Load() && r.Method != "GET" && r.Method != "HEAD" {
		empty, err := identity.Uninitialized(r.Context(), m.Store.DB)
		if err != nil {
			httpapi.Error(w, 503)
			return
		}
		if empty {
			httpapi.JSON(w, 409, httpapi.ErrorResponse{Error: "SetupRequired", ErrorMessage: "Site setup is required."})
			return
		}
		m.needsSetup.Store(false)
	}
	next.ServeHTTP(w, r)
}
