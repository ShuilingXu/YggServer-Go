package identity

import (
	"context"
	"errors"
	"github.com/ShuilingXu/YggServer-Go/plugin"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/audit"
	"github.com/ShuilingXu/YggServer-Go/internal/httpapi"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/redisstore"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
)

type Handler struct {
	Service *Service
	Redis   *redisstore.Store
	Logger  *slog.Logger
}

func (h *Handler) failure(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ErrCredentials), errors.Is(err, ErrDenied), errors.Is(err, plugin.ErrDenied):
		status = http.StatusForbidden
	case errors.Is(err, site.ErrCooldown), errors.Is(err, site.ErrFunds):
		status = http.StatusConflict
	case errors.Is(err, site.ErrPolicy):
		status = http.StatusBadRequest
	case errors.Is(err, ErrConflict):
		status = http.StatusConflict
	case errors.Is(err, ErrInvalid):
		status = http.StatusBadRequest
	case errors.Is(err, ErrUnavailable), errors.Is(err, ErrMail), errors.Is(err, plugin.ErrFailed):
		status = http.StatusServiceUnavailable
	case errors.Is(err, ErrRateLimited):
		status = http.StatusTooManyRequests
		w.Header().Set("Retry-After", "60")
	}
	if (strings.HasPrefix(r.URL.Path, "/api/user/profile/") || r.URL.Path == "/minecraft/profile/skins") && status == http.StatusForbidden {
		status = http.StatusUnauthorized
	}
	if strings.HasPrefix(r.URL.Path, "/authserver/") && status == http.StatusForbidden {
		httpapi.JSON(w, status, httpapi.ErrorResponse{Error: "ForbiddenOperationException", ErrorMessage: "Invalid credentials or token."})
		return
	}
	if errors.Is(err, site.ErrFunds) {
		httpapi.JSON(w, status, httpapi.ErrorResponse{Error: "InsufficientPoints", ErrorMessage: "Insufficient points."})
		return
	}
	if errors.Is(err, plugin.ErrDenied) {
		httpapi.JSON(w, status, httpapi.ErrorResponse{Error: "PluginDenied", ErrorMessage: "Operation rejected by site policy."})
		return
	}
	if errors.Is(err, site.ErrCooldown) {
		httpapi.JSON(w, status, httpapi.ErrorResponse{Error: "AlreadySigned", ErrorMessage: "Check-in cooldown is active."})
		return
	}
	httpapi.Error(w, status)
}

func (h *Handler) limited(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Service == nil || h.Redis == nil {
			h.failure(w, r, ErrUnavailable)
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			ip = "unknown"
		}
		ok, err := h.Redis.Allow(r.Context(), "credentials:"+ip, 20, time.Minute)
		if err != nil {
			h.failure(w, r, ErrUnavailable)
			return
		}
		if !ok {
			w.Header().Set("Retry-After", "60")
			httpapi.Error(w, http.StatusTooManyRequests)
			return
		}
		next(w, r)
	}
}

type tokenContext struct{}

func Current(ctx context.Context) Token { t, _ := ctx.Value(tokenContext{}).(Token); return t }

func (h *Handler) Authorized(kind model.TokenKind, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if h.Service == nil {
			h.failure(w, r, ErrUnavailable)
			return
		}
		value := r.Header.Get("Authorization")
		if !strings.HasPrefix(value, "Bearer ") {
			h.failure(w, r, ErrCredentials)
			return
		}
		t, err := h.Service.Validate(r.Context(), strings.TrimPrefix(value, "Bearer "), nil, kind, false)
		if err != nil {
			h.failure(w, r, err)
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), tokenContext{}, t)))
	}
}

func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	h.registerSite(mux)
	h.registerExternal(mux)
	mux.HandleFunc("GET /api/admin/audit", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		if Current(r.Context()).Account.Permission < model.PermissionAdmin {
			h.failure(w, r, ErrDenied)
			return
		}
		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				h.failure(w, r, ErrInvalid)
				return
			}
			offset = n
		}
		events, err := audit.List(r.Context(), h.Service.Store.DB, h.Service.Store.Dialect, offset)
		if err != nil {
			h.failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, events)
	}))

	mux.HandleFunc("POST /api/account/verification", h.Authorized(model.TokenWeb, h.limited(func(w http.ResponseWriter, r *http.Request) {
		err := h.Service.RequestRecovery(r.Context(), Current(r.Context()).Account.Email, model.RecoveryVerify)
		if err != nil {
			h.failure(w, r, err)
			return
		}
		w.WriteHeader(202)
	})))
	mux.HandleFunc("POST /api/auth/forgot-password", h.limited(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Email string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		err := h.Service.RequestRecovery(r.Context(), body.Email, model.RecoveryReset)
		// 投递失败也维持受理响应，避免通过 SMTP 错误枚举已注册邮箱。
		if err != nil && !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrUnavailable) {
			h.Logger.ErrorContext(r.Context(), "recovery mail delivery failed", "request_id", w.Header().Get("X-Request-ID"))
			err = nil
		}
		if err != nil {
			h.failure(w, r, err)
			return
		}
		w.WriteHeader(202)
	}))
	for _, route := range []string{"POST /api/auth/verify-email", "POST /api/auth/reset-password"} {
		mux.HandleFunc(route, h.limited(func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Token, NewPassword string }
			if !httpapi.Decode(w, r, &body) {
				return
			}
			kind := model.RecoveryVerify
			if r.URL.Path == "/api/auth/reset-password" {
				kind = model.RecoveryReset
			}
			if err := h.Service.CompleteRecovery(r.Context(), body.Token, body.NewPassword, kind); err != nil {
				h.failure(w, r, err)
				return
			}
			w.WriteHeader(204)
		}))
	}

	mux.HandleFunc("GET /api/players", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		profiles, err := h.Service.Store.Profiles(r.Context(), Current(r.Context()).Account.ID)
		if err != nil {
			h.failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, profiles)
	}))
	for _, route := range []string{"POST /api/players", "PUT /api/players/{uuid}", "DELETE /api/players/{uuid}"} {
		mux.HandleFunc(route, h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Name string }
			remove := r.Method == "DELETE"
			if !remove {
				if !httpapi.Decode(w, r, &body) {
					return
				}
				if !ValidPlayer(body.Name) {
					h.failure(w, r, ErrInvalid)
					return
				}
			}
			err := h.Service.Store.SavePlayer(r.Context(), Current(r.Context()).Account, r.PathValue("uuid"), body.Name, remove, h.Service.Now().UTC())
			if err != nil {
				h.failure(w, r, err)
				return
			}
			w.WriteHeader(204)
		}))
	}
	mux.HandleFunc("POST /api/auth/register", h.limited(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Email, Password, PlayerName string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		if err := h.Service.Register(r.Context(), body.Email, body.Password, body.PlayerName); err != nil {
			h.failure(w, r, err)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	for _, path := range []string{"POST /api/auth/login", "POST /authserver/authenticate"} {
		mux.HandleFunc(path, h.limited(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				Username, Password string
				ClientToken        *string
				RequestUser        bool
				Agent              *struct {
					Name    string
					Version int
				}
			}
			if !httpapi.Decode(w, r, &body) {
				return
			}
			kind := model.TokenWeb
			if r.URL.Path == "/authserver/authenticate" {
				kind = model.TokenYggdrasil
				if body.Agent != nil && (body.Agent.Name != "Minecraft" || body.Agent.Version != 1) {
					h.failure(w, r, ErrInvalid)
					return
				}
			}
			response, err := h.Service.Authenticate(r.Context(), body.Username, body.Password, body.ClientToken, kind, body.RequestUser)
			if err != nil {
				h.failure(w, r, err)
				return
			}
			httpapi.JSON(w, 200, response)
		}))
	}
	mux.HandleFunc("POST /api/auth/logout", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		if err := h.Service.Store.Revoke(r.Context(), Current(r.Context()).Hash); err != nil {
			h.failure(w, r, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /api/account", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) { httpapi.JSON(w, 200, Current(r.Context()).Account) }))
	for _, path := range []string{"PUT /api/account/password", "PUT /api/account/email", "DELETE /api/account"} {
		mux.HandleFunc(path, h.Authorized(model.TokenWeb, h.limited(func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Password, NewPassword, Email string }
			if !httpapi.Decode(w, r, &body) {
				return
			}
			if r.URL.Path == "/api/account/email" {
				if body.Email == "" {
					h.failure(w, r, ErrInvalid)
					return
				}
				body.NewPassword = ""
			} else {
				body.Email = ""
			}
			if err := h.Service.UpdateAccount(r.Context(), Current(r.Context()), body.Password, body.NewPassword, body.Email, r.Method == "DELETE"); err != nil {
				h.failure(w, r, err)
				return
			}
			w.WriteHeader(204)
		})))
	}
	mux.HandleFunc("GET /api/admin/users", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		if Current(r.Context()).Account.Permission < model.PermissionAdmin {
			h.failure(w, r, ErrDenied)
			return
		}
		offset := 0
		if value := r.URL.Query().Get("offset"); value != "" {
			var err error
			offset, err = strconv.Atoi(value)
			if err != nil || offset < 0 {
				h.failure(w, r, ErrInvalid)
				return
			}
		}
		users, err := h.Service.Store.Users(r.Context(), offset)
		if err != nil {
			h.failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, users)
	}))
	mux.HandleFunc("PUT /api/admin/users/{uuid}", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			State      model.AccountState
			Permission model.Permission
		}
		if !httpapi.Decode(w, r, &body) {
			return
		}
		if body.State < 0 || body.State > 1 || body.Permission < 0 || body.Permission > 1 {
			h.failure(w, r, ErrInvalid)
			return
		}
		if err := h.Service.Store.Manage(r.Context(), Current(r.Context()).Account.ID, r.PathValue("uuid"), body.State, body.Permission, w.Header().Get("X-Request-ID"), h.Service.Now()); err != nil {
			h.failure(w, r, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("POST /authserver/refresh", h.limited(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			AccessToken     string
			ClientToken     *string
			SelectedProfile *Profile
			RequestUser     bool
		}
		if !httpapi.Decode(w, r, &body) {
			return
		}
		response, err := h.Service.Refresh(r.Context(), body.AccessToken, body.ClientToken, body.SelectedProfile, body.RequestUser)
		if err != nil {
			h.failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, response)
	}))
	for _, path := range []string{"POST /authserver/validate", "POST /authserver/invalidate"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if h.Service == nil {
				h.failure(w, r, ErrUnavailable)
				return
			}
			var body struct {
				AccessToken string
				ClientToken *string
			}
			if !httpapi.Decode(w, r, &body) {
				return
			}
			_, err := h.Service.Validate(r.Context(), body.AccessToken, body.ClientToken, model.TokenYggdrasil, false)
			if r.URL.Path == "/authserver/invalidate" {
				err = h.Service.Store.Revoke(r.Context(), Hash(body.AccessToken))
			}
			if err != nil {
				h.failure(w, r, err)
				return
			}
			w.WriteHeader(204)
		})
	}
	mux.HandleFunc("POST /authserver/signout", h.limited(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Username, Password string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		a, err := h.Service.Credentials(r.Context(), body.Username, body.Password)
		if err == nil {
			err = h.Service.Store.Signout(r.Context(), a)
		}
		if err != nil {
			h.failure(w, r, err)
			return
		}
		w.WriteHeader(204)
	}))
}
