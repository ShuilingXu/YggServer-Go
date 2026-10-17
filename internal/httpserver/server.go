package httpserver

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/config"
	"github.com/ShuilingXu/YggServer-Go/migrations"
)

const Version = "0.1.0-dev"

type Metadata struct {
	Meta               Meta     `json:"meta"`
	SkinDomains        []string `json:"skinDomains"`
	SignaturePublickey string   `json:"signaturePublickey"`
}

type Meta struct {
	ServerName            string            `json:"serverName"`
	ImplementationName    string            `json:"implementationName"`
	ImplementationVersion string            `json:"implementationVersion"`
	Links                 map[string]string `json:"links"`
	NonEmailLogin         bool              `json:"feature.non_email_login"`
}

type ErrorResponse struct {
	Error        string `json:"error"`
	ErrorMessage string `json:"errorMessage"`
}

func New(c config.Config, publicKey string, db *sql.DB, logger *slog.Logger) (http.Handler, error) {
	var salt [32]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return nil, fmt.Errorf("initialize access log privacy: %w", err)
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || u.Hostname() == "" {
		return nil, fmt.Errorf("invalid public url")
	}
	domains := []string{u.Hostname()}
	for _, domain := range c.SkinDomains {
		found := false
		for _, existing := range domains {
			if domain == existing {
				found = true
			}
		}
		if !found {
			domains = append(domains, domain)
		}
	}
	metadata := Metadata{Meta: Meta{
		ServerName: c.ServerName, ImplementationName: "YggServer-Go", ImplementationVersion: Version,
		Links: map[string]string{"homepage": c.PublicURL + "/web/"},
	}, SkinDomains: domains, SignaturePublickey: publicKey}
	mux := http.NewServeMux()
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeError(w, http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, metadata)
	})
	for _, path := range []string{"/healthz", "/readyz"} {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				writeError(w, http.StatusMethodNotAllowed)
				return
			}
			if r.URL.Path == "/readyz" {
				ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
				defer cancel()
				if err := migrations.Check(ctx, db, c.DatabaseDriver); err != nil {
					writeError(w, http.StatusServiceUnavailable)
					return
				}
			}
			writeJSON(w, http.StatusOK, struct {
				Status string `json:"status"`
			}{Status: "ok"})
		})
	}
	if c.WebDir != "" {
		mux.Handle("GET /web/", http.StripPrefix("/web/", http.FileServer(http.Dir(c.WebDir))))
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { writeError(w, http.StatusNotFound) })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := rand.Text()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-Authlib-Injector-API-Location", c.PublicURL+"/")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		recorded := &responseWriter{ResponseWriter: w}
		defer func() {
			if recover() != nil {
				if recorded.status == 0 {
					writeError(recorded, http.StatusInternalServerError)
				}
			}
			if recorded.status == 0 {
				recorded.status = http.StatusOK
			}
			ip, _, err := net.SplitHostPort(r.RemoteAddr)
			if err != nil {
				ip = "unknown"
			}
			digest := hmac.New(sha256.New, salt[:])
			_, _ = digest.Write([]byte(ip))
			// 记录路由模板；未知路径可能含凭证，不把原始路径或 query 写入日志。
			path := r.Pattern
			if path == "/" || path == "" {
				path = "[unmatched]"
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
				slog.String("request_id", requestID), slog.String("method", r.Method), slog.String("path", path),
				slog.Int("status", recorded.status), slog.Int64("dur_ms", time.Since(start).Milliseconds()),
				slog.String("ip_hash", hex.EncodeToString(digest.Sum(nil))))
		}()
		mux.ServeHTTP(recorded, r)
	}), nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int) {
	writeJSON(w, status, ErrorResponse{Error: http.StatusText(status), ErrorMessage: http.StatusText(status)})
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (w *responseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}
