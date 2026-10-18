package httpserver

import (
	"crypto/rsa"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ShuilingXu/YggServer-Go/internal/config"
	"github.com/ShuilingXu/YggServer-Go/internal/httpapi"
	"github.com/ShuilingXu/YggServer-Go/internal/identity"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
	"github.com/ShuilingXu/YggServer-Go/internal/skin"
)

func registerTextures(mux *http.ServeMux, c config.Config, key *rsa.PrivateKey, textures *skin.Service, auth *identity.Handler, logger *slog.Logger) {
	failure := func(w http.ResponseWriter, r *http.Request, err error) {
		status := http.StatusInternalServerError
		switch {
		case errors.Is(err, skin.ErrQuota):
			status = 413
		case errors.Is(err, site.ErrFunds), errors.Is(err, skin.ErrConflict):
			status = 409
		case errors.Is(err, skin.ErrRateLimited):
			status = 429
			w.Header().Set("Retry-After", "60")
		case errors.Is(err, skin.ErrInvalid):
			status = 400
		case errors.Is(err, skin.ErrDenied), errors.Is(err, identity.ErrCredentials):
			status = 403
		case errors.Is(err, skin.ErrMissing):
			status = 404
		case errors.Is(err, skin.ErrUnavailable):
			status = 503
		}
		if errors.Is(err, site.ErrFunds) {
			httpapi.JSON(w, status, httpapi.ErrorResponse{Error: "InsufficientPoints", ErrorMessage: "Insufficient points."})
			return
		}
		httpapi.Error(w, status)
	}
	available := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if textures == nil {
				httpapi.Error(w, 503)
				return
			}
			next(w, r)
		}
	}
	web := func(next http.HandlerFunc) http.HandlerFunc { return available(auth.Authorized(model.TokenWeb, next)) }
	registerManagement(mux, textures, auth, web, failure)
	for _, route := range []string{"GET /api/textures/{uuid}/avatar", "GET /api/textures/{uuid}/preview"} {
		mux.HandleFunc(route, available(func(w http.ResponseWriter, r *http.Request) {
			handle := func(w http.ResponseWriter, r *http.Request) {
				size := 128
				if raw := r.URL.Query().Get("size"); raw != "" {
					n, err := strconv.Atoi(raw)
					if err != nil || n < 16 || n > 512 {
						httpapi.Error(w, 400)
						return
					}
					size = n
				}
				image, err := textures.Render(r.Context(), r.PathValue("uuid"), identity.Current(r.Context()).Account.ID, size, strings.HasSuffix(r.URL.Path, "/avatar"))
				if err != nil {
					failure(w, r, err)
					return
				}
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Cache-Control", "private, no-store")
				_, _ = w.Write(image)
			}
			if r.Header.Get("Authorization") != "" {
				auth.Authorized(model.TokenWeb, handle)(w, r)
			} else {
				handle(w, r)
			}
		}))
	}
	for _, route := range []string{"GET /api/textures", "GET /api/closet", "GET /api/textures/mine"} {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			offset := 0
			if value := r.URL.Query().Get("offset"); value != "" {
				var err error
				offset, err = strconv.Atoi(value)
				if err != nil || offset < 0 {
					httpapi.Error(w, 400)
					return
				}
			}
			mode := "public"
			if r.URL.Path == "/api/closet" {
				mode = "closet"
			}
			if r.URL.Path == "/api/textures/mine" {
				mode = "mine"
			}
			options := skin.ListOptions{Offset: offset, Limit: 50, Query: r.URL.Query().Get("q"), Uploader: r.URL.Query().Get("uploader"), Sort: r.URL.Query().Get("sort")}
			if len(options.Query) > 200 || (options.Uploader != "" && len(options.Uploader) != 32) || (options.Sort != "" && options.Sort != "newest" && options.Sort != "oldest" && options.Sort != "name") {
				httpapi.Error(w, 400)
				return
			}
			if raw := r.URL.Query().Get("limit"); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 1 || n > 100 {
					httpapi.Error(w, 400)
					return
				}
				options.Limit = n
			}
			if raw := r.URL.Query().Get("type"); raw != "" {
				n, err := strconv.Atoi(raw)
				if err != nil || n < 0 || n > 2 {
					httpapi.Error(w, 400)
					return
				}
				kind := model.TextureType(n)
				options.Kind = &kind
			}
			items, total, err := textures.Store.List(r.Context(), identity.Current(r.Context()).Account.ID, mode, options)
			if err != nil {
				failure(w, r, err)
				return
			}
			w.Header().Set("X-Total-Count", strconv.Itoa(total))
			w.Header().Set("X-Limit", strconv.Itoa(options.Limit))
			if len(items) == options.Limit && offset <= total-options.Limit && offset+options.Limit < total {
				next := r.URL.Query()
				next.Set("offset", strconv.Itoa(offset+options.Limit))
				w.Header().Set("Link", "<"+r.URL.Path+"?"+next.Encode()+">; rel=\"next\"")
			}
			httpapi.JSON(w, 200, items)
		})
		if route == "GET /api/textures" {
			mux.HandleFunc(route, available(handler))
		} else {
			mux.HandleFunc(route, web(handler))
		}
	}
	mux.HandleFunc("GET /api/textures/{uuid}", available(func(w http.ResponseWriter, r *http.Request) {
		// 私有条目仅在显式携带 Web 凭证时可见。
		if r.Header.Get("Authorization") != "" {
			auth.Authorized(model.TokenWeb, func(w http.ResponseWriter, r *http.Request) {
				t, err := textures.Store.Get(r.Context(), r.PathValue("uuid"), identity.Current(r.Context()).Account.ID, false)
				if err != nil {
					failure(w, r, err)
					return
				}
				httpapi.JSON(w, 200, t)
			})(w, r)
			return
		}
		t, err := textures.Store.Get(r.Context(), r.PathValue("uuid"), 0, false)
		if err != nil {
			failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, t)
	}))
	upload := func(w http.ResponseWriter, r *http.Request) {
		if textures.Dir == "" {
			httpapi.Error(w, 503)
			return
		}
		maxBytes := c.UploadMaxBytes
		if maxBytes == 0 {
			maxBytes = 1 << 20
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes+(64<<10))
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			httpapi.Error(w, 400)
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			httpapi.Error(w, 400)
			return
		}
		defer file.Close()
		if header.Header.Get("Content-Type") != "image/png" {
			httpapi.Error(w, 415)
			return
		}
		kind := model.TextureSteve
		public := int16(0)
		name := r.FormValue("name")
		player := ""
		token := identity.Current(r.Context())
		if token.Kind == model.TokenYggdrasil {
			player = r.PathValue("uuid")
			if player == "" {
				player = token.PlayerUUID
			}
			if player == "" || token.PlayerUUID != player {
				httpapi.Error(w, 403)
				return
			}
			name = "Minecraft texture"
			switch r.PathValue("type") {
			case "cape":
				kind = model.TextureCape
			case "skin", "":
				variant := r.FormValue("model")
				if variant == "" {
					variant = r.FormValue("variant")
				}
				switch variant {
				case "slim":
					kind = model.TextureAlex
				case "", "classic", "default":
				default:
					httpapi.Error(w, 400)
					return
				}
			default:
				httpapi.Error(w, 400)
				return
			}
		} else {
			value, err := strconv.Atoi(r.FormValue("type"))
			if err != nil || value < 0 || value > 2 {
				httpapi.Error(w, 400)
				return
			}
			kind = model.TextureType(value)
			visibility, err := strconv.Atoi(r.FormValue("public"))
			if err != nil || visibility < 0 || visibility > 1 {
				httpapi.Error(w, 400)
				return
			}
			public = int16(visibility)
		}
		t, err := textures.Upload(r.Context(), token.Account.ID, name, kind, public, file, player)
		if err != nil {
			failure(w, r, err)
			return
		}
		if token.Kind == model.TokenYggdrasil {
			w.WriteHeader(204)
		} else {
			httpapi.JSON(w, 201, t)
		}
	}
	mux.HandleFunc("POST /api/textures", web(upload))
	mux.HandleFunc("GET /api/textures/{uuid}/file", web(func(w http.ResponseWriter, r *http.Request) {
		if textures.Dir == "" {
			httpapi.Error(w, 503)
			return
		}
		t, err := textures.Store.Get(r.Context(), r.PathValue("uuid"), identity.Current(r.Context()).Account.ID, false)
		if err != nil {
			failure(w, r, err)
			return
		}
		f, err := os.Open(filepath.Join(textures.Dir, t.Hash))
		if err != nil {
			httpapi.Error(w, 404)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			httpapi.Error(w, 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "private, no-store")
		http.ServeContent(w, r, t.Hash, info.ModTime(), f)
	}))
	for _, route := range []string{"PUT /api/user/profile/{uuid}/{type}", "POST /minecraft/profile/skins"} {
		mux.HandleFunc(route, available(auth.Authorized(model.TokenYggdrasil, upload)))
	}
	mux.HandleFunc("PUT /api/textures/{uuid}", web(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name   string
			Type   model.TextureType
			Public int16
		}
		if !httpapi.Decode(w, r, &body) {
			return
		}
		if err := textures.Edit(r.Context(), identity.Current(r.Context()).Account.ID, r.PathValue("uuid"), body.Name, body.Type, body.Public); err != nil {
			failure(w, r, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("DELETE /api/textures/{uuid}", web(func(w http.ResponseWriter, r *http.Request) {
		if err := textures.Delete(r.Context(), identity.Current(r.Context()).Account.ID, r.PathValue("uuid")); err != nil {
			failure(w, r, err)
			return
		}
		w.WriteHeader(204)
	}))
	for _, route := range []string{"PUT /api/closet/{uuid}", "DELETE /api/closet/{uuid}"} {
		mux.HandleFunc(route, web(func(w http.ResponseWriter, r *http.Request) {
			var body struct{ Name string }
			if r.Method != "DELETE" && !httpapi.Decode(w, r, &body) {
				return
			}
			if err := textures.Store.Closet(r.Context(), identity.Current(r.Context()).Account.ID, r.PathValue("uuid"), body.Name, r.Method == "DELETE", textures.Now().UTC()); err != nil {
				failure(w, r, err)
				return
			}
			w.WriteHeader(204)
		}))
	}
	mux.HandleFunc("PUT /api/players/{uuid}/textures", web(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Skin, Cape *string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		if body.Skin == nil && body.Cape == nil {
			httpapi.Error(w, 400)
			return
		}
		if err := textures.Store.Bind(r.Context(), identity.Current(r.Context()).Account.ID, r.PathValue("uuid"), body.Skin, body.Cape, textures.Now().UTC()); err != nil {
			failure(w, r, err)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("DELETE /api/user/profile/{uuid}/{type}", available(auth.Authorized(model.TokenYggdrasil, func(w http.ResponseWriter, r *http.Request) {
		t := identity.Current(r.Context())
		if t.PlayerUUID != r.PathValue("uuid") {
			httpapi.Error(w, 403)
			return
		}
		empty := ""
		var sk, ca *string
		switch r.PathValue("type") {
		case "skin":
			sk = &empty
		case "cape":
			ca = &empty
		default:
			httpapi.Error(w, 400)
			return
		}
		if err := textures.Store.Bind(r.Context(), t.Account.ID, t.PlayerUUID, sk, ca, textures.Now().UTC()); err != nil {
			failure(w, r, err)
			return
		}
		w.WriteHeader(204)
	})))
	mux.HandleFunc("GET /textures/{hash}", available(func(w http.ResponseWriter, r *http.Request) {
		if textures.Dir == "" {
			httpapi.Error(w, 503)
			return
		}
		hash := r.PathValue("hash")
		if !skin.ValidHash(hash) {
			httpapi.Error(w, 404)
			return
		}
		allowed, err := textures.Store.CanDistribute(r.Context(), hash)
		if err != nil {
			failure(w, r, err)
			return
		}
		if !allowed {
			httpapi.Error(w, 404)
			return
		}
		f, err := os.Open(filepath.Join(textures.Dir, hash))
		if err != nil {
			httpapi.Error(w, 404)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			httpapi.Error(w, 500)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("ETag", `"`+hash+`"`)
		// 私有或被撤下的文件每次访问都重新检查数据库授权，避免永久公共缓存绕过隐私变更。
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		http.ServeContent(w, r, hash, info.ModTime(), f)
	}))
	mux.HandleFunc("GET /sessionserver/session/minecraft/profile/{uuid}", available(func(w http.ResponseWriter, r *http.Request) {
		response, err := textures.Profile(r.Context(), r.PathValue("uuid"), c.PublicURL, key, r.URL.Query().Get("unsigned") == "false")
		if errors.Is(err, skin.ErrMissing) {
			w.WriteHeader(204)
			return
		}
		if err != nil {
			failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, response)
	}))
	mux.HandleFunc("POST /api/profiles/minecraft", available(func(w http.ResponseWriter, r *http.Request) {
		var names []string
		if !httpapi.Decode(w, r, &names) {
			return
		}
		if len(names) > 100 {
			httpapi.Error(w, 400)
			return
		}
		profiles, err := textures.Store.Lookup(r.Context(), names)
		if err != nil {
			failure(w, r, err)
			return
		}
		httpapi.JSON(w, 200, profiles)
	}))
	mux.HandleFunc("POST /sessionserver/session/minecraft/join", available(func(w http.ResponseWriter, r *http.Request) {
		if auth.Redis == nil || auth.Service == nil {
			httpapi.Error(w, 503)
			return
		}
		var body struct{ AccessToken, SelectedProfile, ServerID string }
		if !httpapi.Decode(w, r, &body) {
			return
		}
		if body.ServerID == "" || len(body.ServerID) > 256 {
			httpapi.Error(w, 400)
			return
		}
		t, err := auth.Service.Validate(r.Context(), body.AccessToken, nil, model.TokenYggdrasil, false)
		if err != nil {
			failure(w, r, err)
			return
		}
		if t.PlayerUUID == "" || t.PlayerUUID != body.SelectedProfile {
			httpapi.Error(w, 403)
			return
		}
		a, err := textures.Store.Appearance(r.Context(), t.PlayerUUID)
		if err != nil {
			failure(w, r, err)
			return
		}
		ip, _, err := net.SplitHostPort(r.RemoteAddr)
		parsedIP := net.ParseIP(ip)
		if err != nil || parsedIP == nil {
			httpapi.Error(w, 400)
			return
		}
		if err := auth.Redis.RecordJoin(r.Context(), body.ServerID, a.Profile.Name, t.Hash, identity.Hash(parsedIP.String())); err != nil {
			httpapi.Error(w, 503)
			return
		}
		w.WriteHeader(204)
	}))
	mux.HandleFunc("GET /sessionserver/session/minecraft/hasJoined", available(func(w http.ResponseWriter, r *http.Request) {
		if auth.Redis == nil || auth.Service == nil {
			httpapi.Error(w, 503)
			return
		}
		serverID, name, ip := r.URL.Query().Get("serverId"), r.URL.Query().Get("username"), r.URL.Query().Get("ip")
		if serverID == "" || len(serverID) > 256 || !identity.ValidPlayer(name) {
			w.WriteHeader(204)
			return
		}
		ipHash := ""
		if ip != "" {
			parsed := net.ParseIP(ip)
			if parsed == nil {
				w.WriteHeader(204)
				return
			}
			ipHash = identity.Hash(parsed.String())
		}
		hash, err := auth.Redis.ConsumeJoin(r.Context(), serverID, name, ipHash)
		if err != nil {
			httpapi.Error(w, 503)
			return
		}
		if hash == "" {
			w.WriteHeader(204)
			return
		}
		t, err := auth.Service.JoinedToken(r.Context(), hash)
		if err != nil {
			w.WriteHeader(204)
			return
		}
		response, err := textures.Profile(r.Context(), t.PlayerUUID, c.PublicURL, key, true)
		if err != nil || response.Name != name {
			w.WriteHeader(204)
			return
		}
		httpapi.JSON(w, 200, response)
	}))
}
