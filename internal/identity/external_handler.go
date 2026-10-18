package identity

import (
	"github.com/ShuilingXu/YggServer-Go/internal/httpapi"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"golang.org/x/crypto/bcrypt"
	"net/http"
)

func (h *Handler) registerExternal(mux *http.ServeMux) {
	mux.HandleFunc("GET /oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, "/web/#/oidc/callback?"+r.URL.RawQuery, http.StatusFound)
	})
	for _, route := range []string{"POST /api/auth/oidc/start", "POST /api/account/oidc/start"} {
		start := h.limited(func(w http.ResponseWriter, r *http.Request) {
			var t *Token
			if r.URL.Path == "/api/account/oidc/start" {
				value := Current(r.Context())
				t = &value
			}
			result, err := h.Service.StartExternal(r.Context(), t)
			if err != nil {
				h.failure(w, r, err)
				return
			}
			httpapi.JSON(w, 200, result)
		})
		if route == "POST /api/account/oidc/start" {
			start = h.Authorized(model.TokenWeb, start)
		}
		mux.HandleFunc(route, start)
	}
	mux.HandleFunc("POST /api/auth/oidc/complete", h.limited(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ State, Code string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		result, err := h.Service.CompleteExternal(r.Context(), body.State, body.Code, w.Header().Get("X-Request-ID"))
		if err != nil {
			h.failure(w, r, err)
			return
		}
		h.Logger.InfoContext(r.Context(), "external login completed", "request_id", w.Header().Get("X-Request-ID"))
		httpapi.JSON(w, 200, result)
	}))
	mux.HandleFunc("GET /api/account/oidc", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		result, err := h.Service.Store.ExternalIdentities(r.Context(), Current(r.Context()).Account.ID)
		if err != nil {
			h.failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, result)
	}))
	mux.HandleFunc("DELETE /api/account/oidc/{uuid}", h.Authorized(model.TokenWeb, h.limited(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Password string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		a := Current(r.Context()).Account
		if bcrypt.CompareHashAndPassword(a.PasswordHash, []byte(body.Password)) != nil {
			h.failure(w, r, ErrCredentials)
			return
		}
		if err := h.Service.Store.RemoveExternal(r.Context(), a, r.PathValue("uuid"), w.Header().Get("X-Request-ID"), h.Service.Now().UTC()); err != nil {
			h.failure(w, r, err)
			return
		}
		h.Logger.InfoContext(r.Context(), "external identity removed", "account_id", a.ID, "request_id", w.Header().Get("X-Request-ID"))
		w.WriteHeader(204)
	})))
}
