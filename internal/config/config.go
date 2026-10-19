package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/plugin"
	"io"
	"net"
	"net/mail"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Plugins          map[string]plugin.Config
	OIDCClientSecret string
	ListenAddr       string
	PublicURL        string
	ServerName       string
	SkinDomains      []string
	SigningKeyFile   string
	DatabaseDriver   string
	DatabaseDSN      string
	WebDir           string
	RedisURL         string
	RedisPrefix      string
	TexturesDir      string
	UUIDStrategy     model.UUIDStrategy
	SMTPAddr         string
	SMTPMode         string
	SMTPUser         string
	SMTPPassword     string
	MailFrom         string
	VerificationTTL  time.Duration
	ResetTTL         time.Duration
	UploadLimit      int
	UploadMaxBytes   int64
	StorageQuota     int64
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
	switch value("YGG_UUID_STRATEGY", "v3") {
	case "v3":
		c.UUIDStrategy = model.UUIDOffline
	case "v4":
		c.UUIDStrategy = model.UUIDRandom
	default:
		return Config{}, fmt.Errorf("invalid YGG_UUID_STRATEGY")
	}
	c.OIDCClientSecret = os.Getenv("YGG_OIDC_CLIENT_SECRET")
	c.SMTPAddr = os.Getenv("YGG_SMTP_ADDR")
	c.SMTPMode = value("YGG_SMTP_MODE", "starttls")
	c.SMTPUser = os.Getenv("YGG_SMTP_USER")
	c.SMTPPassword = os.Getenv("YGG_SMTP_PASSWORD")
	c.MailFrom = os.Getenv("YGG_MAIL_FROM")
	if c.SMTPMode != "starttls" && c.SMTPMode != "tls" && c.SMTPMode != "plain" {
		return Config{}, fmt.Errorf("invalid YGG_SMTP_MODE")
	}
	if c.SMTPAddr != "" {
		host, port, err := net.SplitHostPort(c.SMTPAddr)
		portNumber, portErr := strconv.Atoi(port)
		if err != nil || host == "" || c.PublicURL == "" || portErr != nil || portNumber < 1 || portNumber > 65535 {
			return Config{}, fmt.Errorf("mail requires valid YGG_SMTP_ADDR and YGG_PUBLIC_URL")
		}
		if c.SMTPMode == "plain" && host != "localhost" && (net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback()) {
			return Config{}, fmt.Errorf("plain smtp requires loopback")
		}
		if c.MailFrom == "" || strings.ContainsAny(c.MailFrom, "\r\n") {
			return Config{}, fmt.Errorf("invalid YGG_MAIL_FROM")
		}
		if _, err := mail.ParseAddress(c.MailFrom); err != nil {
			return Config{}, fmt.Errorf("invalid YGG_MAIL_FROM")
		}
	} else if c.SMTPUser != "" || c.SMTPPassword != "" || c.MailFrom != "" {
		return Config{}, fmt.Errorf("mail configuration requires YGG_SMTP_ADDR")
	}
	if (c.SMTPUser == "") != (c.SMTPPassword == "") {
		return Config{}, fmt.Errorf("smtp credentials must be configured together")
	}
	c.VerificationTTL = 24 * time.Hour
	c.ResetTTL = time.Hour
	for _, entry := range []struct {
		key    string
		target *time.Duration
	}{{"YGG_VERIFICATION_TTL", &c.VerificationTTL}, {"YGG_RESET_TTL", &c.ResetTTL}} {
		if raw := os.Getenv(entry.key); raw != "" {
			duration, err := time.ParseDuration(raw)
			if err != nil || duration < time.Minute || duration > 7*24*time.Hour {
				return Config{}, fmt.Errorf("invalid %s", entry.key)
			}
			*entry.target = duration
		}
	}
	c.UploadMaxBytes = 1 << 20
	for _, entry := range []struct {
		key    string
		target *int64
		max    int64
	}{{"YGG_UPLOAD_MAX_BYTES", &c.UploadMaxBytes, 16 << 20}, {"YGG_STORAGE_QUOTA_BYTES", &c.StorageQuota, 1 << 50}} {
		if raw := os.Getenv(entry.key); raw != "" {
			n, err := strconv.ParseInt(raw, 10, 64)
			if err != nil || n < 0 || n > entry.max || (entry.key == "YGG_UPLOAD_MAX_BYTES" && n == 0) {
				return Config{}, fmt.Errorf("invalid %s", entry.key)
			}
			*entry.target = n
		}
	}
	if raw := os.Getenv("YGG_UPLOADS_PER_MINUTE"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 10000 {
			return Config{}, fmt.Errorf("invalid YGG_UPLOADS_PER_MINUTE")
		}
		c.UploadLimit = n
	}

	if path := os.Getenv("YGG_PLUGINS_FILE"); path != "" {
		f, err := os.Open(path)
		if err != nil {
			return Config{}, fmt.Errorf("cannot open YGG_PLUGINS_FILE")
		}
		data, readErr := io.ReadAll(io.LimitReader(f, 65537))
		f.Close()
		if readErr != nil || len(data) > 65536 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
			return Config{}, fmt.Errorf("invalid YGG_PLUGINS_FILE")
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&c.Plugins) != nil || decoder.Decode(new(any)) != io.EOF {
			return Config{}, fmt.Errorf("invalid YGG_PLUGINS_FILE")
		}
	}
	return c, nil
}

func value(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
