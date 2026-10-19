package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeploymentConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		valid bool
	}{
		{"invalid uuid strategy", map[string]string{"YGG_UUID_STRATEGY": "v5"}, false},
		{"valid random uuid", map[string]string{"YGG_UUID_STRATEGY": "v4"}, true},
		{"negative storage quota", map[string]string{"YGG_STORAGE_QUOTA_BYTES": "-1"}, false},
		{"oversized upload", map[string]string{"YGG_UPLOAD_MAX_BYTES": "9999999999"}, false},
		{"remote plain smtp", map[string]string{"YGG_SMTP_ADDR": "smtp.example.com:25", "YGG_SMTP_MODE": "plain", "YGG_PUBLIC_URL": "https://skin.example.com", "YGG_MAIL_FROM": "noreply@example.com"}, false},
		{"expired reset window", map[string]string{"YGG_RESET_TTL": "-1h"}, false},
		{"cdn hostname", map[string]string{"YGG_SKIN_DOMAINS": "textures.example.com,.example.net"}, true},
		{"public origin", map[string]string{"YGG_PUBLIC_URL": "https://skin.example.com/"}, true},
		{"mysql", map[string]string{"YGG_DATABASE_DRIVER": "mysql"}, true},
		{"redis tls", map[string]string{"YGG_REDIS_URL": "rediss://user:sensitive-credential@localhost:6379/1"}, true},
		{"invalid redis scheme", map[string]string{"YGG_REDIS_URL": "https://user:sensitive-credential@localhost"}, false},
		{"invalid redis address", map[string]string{"YGG_REDIS_URL": "redis://"}, false},
		{"invalid redis namespace", map[string]string{"YGG_REDIS_PREFIX": "bad\nkey"}, false},
		{"credential in origin", map[string]string{"YGG_PUBLIC_URL": "https://user:sensitive-credential@skin.example.com"}, false},
		{"query in origin", map[string]string{"YGG_PUBLIC_URL": "https://skin.example.com/?secret=sensitive-credential"}, false},
		{"unsupported driver", map[string]string{"YGG_DATABASE_DRIVER": "sqlite"}, false},
		{"url instead of domain", map[string]string{"YGG_SKIN_DOMAINS": "https://textures.example.com"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"YGG_LISTEN_ADDR", "YGG_PUBLIC_URL", "YGG_SERVER_NAME", "YGG_SKIN_DOMAINS", "YGG_SIGNING_KEY_FILE", "YGG_DATABASE_DRIVER", "YGG_DATABASE_DSN", "YGG_WEB_DIR", "YGG_REDIS_URL", "YGG_REDIS_PREFIX", "YGG_UUID_STRATEGY", "YGG_STORAGE_QUOTA_BYTES", "YGG_UPLOAD_MAX_BYTES", "YGG_UPLOADS_PER_MINUTE", "YGG_SMTP_ADDR", "YGG_SMTP_MODE", "YGG_SMTP_USER", "YGG_SMTP_PASSWORD", "YGG_MAIL_FROM", "YGG_RESET_TTL", "YGG_VERIFICATION_TTL"} {
				t.Setenv(key, "")
			}
			for key, value := range tc.env {
				t.Setenv(key, value)
			}
			_, err := Load()
			if (err == nil) != tc.valid {
				t.Fatalf("valid = %t, want %t; error = %v", err == nil, tc.valid, err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive-credential") {
				t.Fatal("configuration error leaks credentials")
			}
		})
	}
}

func TestPluginConfigurationFile(t *testing.T) {
	for _, data := range []string{`null`, `[]`, `{"activity":{"enabled":1,"secret":"sensitive-credential"}}`, `{}`, `{"activity":{"enabled":1,"options":{}}}`} {
		t.Run(data, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "plugins.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("YGG_PLUGINS_FILE", path)
			_, err := Load()
			valid := data == `{}` || data == `{"activity":{"enabled":1,"options":{}}}`
			if (err == nil) != valid {
				t.Fatalf("valid=%t, error=%v", valid, err)
			}
			if err != nil && strings.Contains(err.Error(), "sensitive-credential") {
				t.Fatal("configuration leaked")
			}
		})
	}
}
