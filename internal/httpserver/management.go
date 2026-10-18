package httpserver

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/ShuilingXu/YggServer-Go/internal/httpapi"
	"github.com/ShuilingXu/YggServer-Go/internal/identity"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/skin"
)

func registerManagement(mux *http.ServeMux, textures *skin.Service, auth *identity.Handler, web func(http.HandlerFunc) http.HandlerFunc, failure func(http.ResponseWriter, *http.Request, error)) {
	mux.HandleFunc("POST /api/textures/{uuid}/reports", web(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Reason string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		if err := textures.Report(r.Context(), identity.Current(r.Context()).Account.ID, r.PathValue("uuid"), body.Reason); err != nil {
			failure(w, r, err)
			return
		}
		w.WriteHeader(201)
	}))
	admin := func(next http.HandlerFunc) http.HandlerFunc {
		return web(func(w http.ResponseWriter, r *http.Request) {
			if identity.Current(r.Context()).Account.Permission < model.PermissionAdmin {
				httpapi.Error(w, 403)
				return
			}
			next(w, r)
		})
	}
	mux.HandleFunc("GET /api/admin/reports", admin(func(w http.ResponseWriter, r *http.Request) {
		offset := 0
		if raw := r.URL.Query().Get("offset"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				httpapi.Error(w, 400)
				return
			}
			offset = n
		}
		reports, err := textures.Store.Reports(r.Context(), offset)
		if err != nil {
			failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, reports)
	}))
	for _, route := range []string{"PUT /api/admin/textures/{uuid}/state", "PUT /api/admin/reports/{uuid}"} {
		mux.HandleFunc(route, admin(func(w http.ResponseWriter, r *http.Request) {
			var body struct {
				State      model.TextureState
				Resolution model.ReportState
				Note       string
			}
			if !httpapi.Decode(w, r, &body) {
				return
			}
			report := strings.HasPrefix(r.URL.Path, "/api/admin/reports/")
			if report && body.Resolution == model.ReportResolved && body.State != model.TextureHidden {
				httpapi.Error(w, 400)
				return
			}
			if err := textures.Store.Moderate(r.Context(), identity.Current(r.Context()).Account.ID, r.PathValue("uuid"), body.State, report, body.Resolution, body.Note, w.Header().Get("X-Request-ID"), textures.Now().UTC()); err != nil {
				failure(w, r, err)
				return
			}
			w.WriteHeader(204)
		}))
	}
}
