package config

import (
	"strings"
	"testing"
)

func TestDeploymentConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name  string
		env   map[string]string
		valid bool
	}{
		{"cdn hostname", map[string]string{"YGG_SKIN_DOMAINS": "textures.example.com,.example.net"}, true},
		{"public origin", map[string]string{"YGG_PUBLIC_URL": "https://skin.example.com/"}, true},
		{"mysql", map[string]string{"YGG_DATABASE_DRIVER": "mysql"}, true},
		{"credential in origin", map[string]string{"YGG_PUBLIC_URL": "https://user:sensitive-credential@skin.example.com"}, false},
		{"query in origin", map[string]string{"YGG_PUBLIC_URL": "https://skin.example.com/?secret=sensitive-credential"}, false},
		{"unsupported driver", map[string]string{"YGG_DATABASE_DRIVER": "sqlite"}, false},
		{"url instead of domain", map[string]string{"YGG_SKIN_DOMAINS": "https://textures.example.com"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"YGG_LISTEN_ADDR", "YGG_PUBLIC_URL", "YGG_SERVER_NAME", "YGG_SKIN_DOMAINS", "YGG_SIGNING_KEY_FILE", "YGG_DATABASE_DRIVER", "YGG_DATABASE_DSN", "YGG_WEB_DIR"} {
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
