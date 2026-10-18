package httpserver_test

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShuilingXu/YggServer-Go/internal/config"
	"github.com/ShuilingXu/YggServer-Go/internal/httpserver"
	"github.com/ShuilingXu/YggServer-Go/internal/signing"
)

func TestMetadataAndSafeHTTPBoundary(t *testing.T) {
	keyFile := filepath.Join(t.TempDir(), "signing.pem")
	if err := signing.Generate(keyFile); err != nil {
		t.Fatal(err)
	}
	key, err := signing.Load(keyFile)
	if err != nil {
		t.Fatal(err)
	}
	public, err := signing.PublicPEM(key)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler, err := httpserver.New(config.Config{PublicURL: "https://skin.example.com", ServerName: "Example", DatabaseDriver: "postgres"}, public, key, nil, nil, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{"GET", "/", 200}, {"POST", "/", 405}, {"GET", "/healthz", 200},
		{"GET", "/readyz", 503}, {"GET", "/authserver/authenticate", 404},
		{"GET", "/sensitive-path?accessToken=sensitive-query", 404},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			logs.Reset()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader("sensitive-body"))
			req.Header.Set("Authorization", "sensitive-authorization")
			req.Header.Set("Cookie", "sensitive-cookie")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, req)
			if recorder.Code != tc.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tc.status)
			}
			if recorder.Header().Get("X-Authlib-Injector-API-Location") != "https://skin.example.com/" {
				t.Fatal("missing API discovery header")
			}
			if tc.path == "/" && tc.method == http.MethodGet {
				var metadata httpserver.Metadata
				if err := json.Unmarshal(recorder.Body.Bytes(), &metadata); err != nil {
					t.Fatal(err)
				}
				block, _ := pem.Decode([]byte(metadata.SignaturePublickey))
				if block == nil || block.Type != "PUBLIC KEY" {
					t.Fatal("missing protocol public key")
				}
				parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
				if err != nil {
					t.Fatal(err)
				}
				pub, ok := parsed.(*rsa.PublicKey)
				if !ok || pub.N.BitLen() != 4096 || pub.N.Cmp(key.N) != 0 {
					t.Fatal("published key does not match persistent signing key")
				}
				if len(metadata.SkinDomains) != 1 || metadata.SkinDomains[0] != "skin.example.com" {
					t.Fatal("missing texture domain whitelist")
				}
			} else if tc.status >= 400 {
				var body httpserver.ErrorResponse
				if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Error == "" || body.ErrorMessage == "" {
					t.Fatal("invalid protocol error response")
				}
			}
			if strings.Contains(logs.String(), "sensitive-") || strings.Contains(logs.String(), "192.0.2.1") {
				t.Fatal("request credentials or raw IP leaked into log")
			}
			if strings.Count(strings.TrimSpace(logs.String()), "\n") != 0 {
				t.Fatal("expected exactly one access log")
			}
			var entry map[string]any
			if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"request_id", "method", "path", "status", "dur_ms", "ip_hash"} {
				if _, ok := entry[field]; !ok {
					t.Fatalf("missing access log field %s", field)
				}
			}
		})
	}
}
