package httpserver

import (
	"net/http"

	"github.com/ShuilingXu/YggServer-Go/internal/httpapi"
	"github.com/ShuilingXu/YggServer-Go/internal/identity"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/plugin"
)

func registerPlugins(mux *http.ServeMux, registry *plugin.Registry, auth *identity.Handler) {
	owner := func(next http.HandlerFunc) http.HandlerFunc {
		return auth.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
			if identity.Current(r.Context()).Account.Permission != model.PermissionOwner {
				httpapi.Error(w, http.StatusForbidden)
				return
			}
			next(w, r)
		})
	}
	mux.HandleFunc("GET /api/admin/plugins", owner(func(w http.ResponseWriter, r *http.Request) {
		httpapi.JSON(w, 200, registry.List())
	}))
	for _, info := range registry.List() {
		if info.Enabled != 1 {
			continue
		}
		for _, path := range info.Endpoints {
			mux.HandleFunc("/api/plugins/"+info.ID+path, owner(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet && r.Method != http.MethodHead {
					w.Header().Set("Allow", "GET, HEAD")
					httpapi.Error(w, http.StatusMethodNotAllowed)
					return
				}
				a := identity.Current(r.Context()).Account
				value, err := registry.Query(r.Context(), info.ID, path, plugin.Actor{AccountID: a.ID, UUID: a.UUID})
				if err != nil {
					httpapi.Error(w, 503)
					return
				}
				httpapi.JSON(w, 200, value)
			}))
		}
	}
}
