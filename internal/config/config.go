package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
)

type Config struct {
	ListenAddr     string
	PublicURL      string
	ServerName     string
	SkinDomains    []string
	SigningKeyFile string
	DatabaseDriver string
	DatabaseDSN    string
	WebDir         string
	RedisURL       string
	RedisPrefix    string
	TexturesDir    string
}

func Load() (Config, error) {
	c := Config{
		ListenAddr:     value("YGG_LISTEN_ADDR", "127.0.0.1:8080"),
		PublicURL:      os.Getenv("YGG_PUBLIC_URL"),
		ServerName:     value("YGG_SERVER_NAME", "YggServer-Go"),
		SigningKeyFile: os.Getenv("YGG_SIGNING_KEY_FILE"),
		DatabaseDriver: value("YGG_DATABASE_DRIVER", "postgres"),
		DatabaseDSN:    os.Getenv("YGG_DATABASE_DSN"),
		WebDir:         os.Getenv("YGG_WEB_DIR"),
		RedisURL:       os.Getenv("YGG_REDIS_URL"),
		RedisPrefix:    value("YGG_REDIS_PREFIX", "ygg:"),
		TexturesDir:    os.Getenv("YGG_TEXTURES_DIR"),
	}
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return Config{}, fmt.Errorf("invalid YGG_LISTEN_ADDR")
	}
	if c.PublicURL != "" {
		u, err := url.Parse(c.PublicURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
			return Config{}, fmt.Errorf("YGG_PUBLIC_URL must be an http(s) origin")
		}
		c.PublicURL = strings.TrimRight(c.PublicURL, "/")
	}
	if c.DatabaseDriver != "postgres" && c.DatabaseDriver != "mysql" {
		return Config{}, fmt.Errorf("YGG_DATABASE_DRIVER must be postgres or mysql")
	}
	if c.RedisURL != "" {
		u, err := url.Parse(c.RedisURL)
		if err != nil || (u.Scheme != "redis" && u.Scheme != "rediss") || u.Hostname() == "" || u.Fragment != "" {
			return Config{}, fmt.Errorf("invalid YGG_REDIS_URL")
		}
	}
	if len(c.RedisPrefix) > 64 || strings.ContainsAny(c.RedisPrefix, " \t\r\n") {
		return Config{}, fmt.Errorf("invalid YGG_REDIS_PREFIX")
	}
	for _, domain := range strings.Split(os.Getenv("YGG_SKIN_DOMAINS"), ",") {
		domain = strings.TrimSpace(domain)
		if domain == "" {
			continue
		}
		if strings.ContainsAny(domain, "/:@?# \\ \t\r\n") {
			return Config{}, fmt.Errorf("YGG_SKIN_DOMAINS must contain hostnames without ports")
		}
		c.SkinDomains = append(c.SkinDomains, domain)
	}
	return c, nil
}

func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
