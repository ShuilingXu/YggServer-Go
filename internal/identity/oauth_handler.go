package identity

import (
	"errors"
	"github.com/ShuilingXu/YggServer-Go/internal/httpapi"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
	"net/http"
	"strings"
)

func (h *Handler) oauthEnabled(w http.ResponseWriter, r *http.Request) bool {
	if h.Service == nil {
		h.failure(w, r, ErrUnavailable)
		return false
	}
	c, err := site.Read(r.Context(), h.Service.Store.DB)
	if err != nil {
		h.failure(w, r, err)
		return false
	}
	if c.OAuthEnabled != 1 {
		h.failure(w, r, ErrDenied)
		return false
	}
	return true
}

func (h *Handler) registerOAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET /oauth/authorize", func(w http.ResponseWriter, r *http.Request) {
		if !h.oauthEnabled(w, r) {
			return
		}
		w.Header().Set("Referrer-Policy", "no-referrer")
		http.Redirect(w, r, "/web/#/oauth/authorize?"+r.URL.RawQuery, http.StatusFound)
	})
	mux.HandleFunc("GET /api/oauth/client", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		if !h.oauthEnabled(w, r) {
			return
		}
		c, err := h.Service.Store.OAuthClient(r.Context(), r.URL.Query().Get("client_id"))
		if err != nil {
			h.failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, c)
	}))
	mux.HandleFunc("POST /api/oauth/authorize", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		if !h.oauthEnabled(w, r) {
			return
		}
		var q OAuthRequest
		if !httpapi.Decode(w, r, &q) {
			return
		}
		t := Current(r.Context())
		redirect, err := h.Service.Store.AuthorizeOAuth(r.Context(), t, q, h.Service.Now())
		if err != nil {
			h.failure(w, r, err)
			return
		}
		h.Logger.InfoContext(r.Context(), "oauth consent completed", "account_id", t.Account.ID, "client_id", q.ClientID, "approved", q.Approve, "request_id", w.Header().Get("X-Request-ID"))
		httpapi.JSON(w, 200, map[string]string{"redirect": redirect})
	}))
	mux.HandleFunc("POST /oauth/token", h.RateLimit(func(w http.ResponseWriter, r *http.Request) {
		if !h.oauthEnabled(w, r) {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "authorization_code" {
			httpapi.JSON(w, 400, map[string]string{"error": "invalid_request"})
			return
		}
		token, err := h.Service.Store.ExchangeOAuth(r.Context(), r.PostForm.Get("code"), r.PostForm.Get("client_id"), r.PostForm.Get("redirect_uri"), r.PostForm.Get("code_verifier"), h.Service.Now())
		if errors.Is(err, ErrCredentials) {
			httpapi.JSON(w, 400, map[string]string{"error": "invalid_grant"})
			return
		}
		if err != nil {
			h.failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, token)
	}))
	for _, route := range []string{"GET /oauth/userinfo", "GET /oauth/players"} {
		mux.HandleFunc(route, func(w http.ResponseWriter, r *http.Request) {
			if !h.oauthEnabled(w, r) {
				return
			}
			value := r.Header.Get("Authorization")
			if !strings.HasPrefix(value, "Bearer ") {
				h.failure(w, r, ErrCredentials)
				return
			}
			scope := OAuthUserRead
			if r.URL.Path == "/oauth/players" {
				scope = OAuthPlayerRead
			}
			id, uuid, err := h.Service.Store.OAuthIdentity(r.Context(), strings.TrimPrefix(value, "Bearer "), scope, h.Service.Now())
			if err != nil {
				h.failure(w, r, err)
				return
			}
			if scope == OAuthUserRead {
				httpapi.JSON(w, 200, map[string]string{"sub": uuid})
				return
			}
			players, err := h.Service.Store.Profiles(r.Context(), id)
			if err != nil {
				h.failure(w, r, err)
				return
			}
			httpapi.JSON(w, 200, players)
		})
	}
	for _, route := range []string{"GET /api/account/oauth", "GET /api/admin/oauth/clients"} {
		mux.HandleFunc(route, h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
			a := Current(r.Context()).Account
			id := a.ID
			if r.URL.Path == "/api/admin/oauth/clients" {
				if a.Permission != model.PermissionOwner {
					h.failure(w, r, ErrDenied)
					return
				}
				id = 0
			}
			clients, err := h.Service.Store.OAuthClients(r.Context(), id, h.Service.Now())
			if err != nil {
				h.failure(w, r, err)
				return
			}
			httpapi.JSON(w, 200, clients)
		}))
	}
	mux.HandleFunc("DELETE /api/account/oauth/{uuid}", h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
		a := Current(r.Context()).Account
		if err := h.Service.Store.RevokeOAuth(r.Context(), a.ID, r.PathValue("uuid")); err != nil {
			h.failure(w, r, err)
			return
		}
		h.Logger.InfoContext(r.Context(), "oauth consent revoked", "account_id", a.ID, "request_id", w.Header().Get("X-Request-ID"))
		w.WriteHeader(204)
	}))
	for _, route := range []string{"POST /api/admin/oauth/clients", "DELETE /api/admin/oauth/clients/{uuid}"} {
		mux.HandleFunc(route, h.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
			c := OAuthClient{ID: r.PathValue("uuid")}
			remove := r.Method == "DELETE"
			if !remove {
				if !httpapi.Decode(w, r, &c) {
					return
				}
				c.ID = model.UUID()
			}
			a := Current(r.Context()).Account
			if err := h.Service.Store.ManageClient(r.Context(), a, c, remove, w.Header().Get("X-Request-ID"), h.Service.Now()); err != nil {
				h.failure(w, r, err)
				return
			}
			h.Logger.InfoContext(r.Context(), "oauth client updated", "account_id", a.ID, "removed", remove, "request_id", w.Header().Get("X-Request-ID"))
			if remove {
				w.WriteHeader(204)
			} else {
				httpapi.JSON(w, 201, c)
			}
		}))
	}
}
