package site

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var ErrPolicy = errors.New("site policy denied")
var ErrFunds = errors.New("insufficient points")
var ErrCooldown = errors.New("check-in cooldown")

type Settings struct {
	OIDCEnabled       int16  `json:"oidcEnabled"`
	OIDCIssuer        string `json:"oidcIssuer"`
	OIDCClientID      string `json:"oidcClientId"`
	OIDCName          string `json:"oidcName"`
	SkinDomains       string `json:"skinDomains"`
	UploadCost        int64  `json:"uploadCost"`
	PrivateUploadCost int64  `json:"privateUploadCost"`
	ClosetCost        int64  `json:"closetCost"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Announcement      string `json:"announcement"`
	Registration      int16  `json:"registration"`
	EmailMode         int16  `json:"emailMode"`
	EmailDomains      string `json:"emailDomains"`
	MaxPlayers        int    `json:"maxPlayers"`
	SingleSession     int16  `json:"singleSession"`
	SignEnabled       int16  `json:"signEnabled"`
	SignHours         int    `json:"signHours"`
	SignMin           int64  `json:"signMin"`
	SignMax           int64  `json:"signMax"`
	InitialPoints     int64  `json:"initialPoints"`
	PlayerCost        int64  `json:"playerCost"`
	OAuthEnabled      int16  `json:"oauthEnabled"`
}

func Defaults() Settings {
	return Settings{Name: "YggServer-Go", Registration: 1, MaxPlayers: 20, SignEnabled: 1, SignHours: 24, SignMin: 10, SignMax: 100, InitialPoints: 1000, PlayerCost: 100}
}

type Query interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func Read(ctx context.Context, db Query) (Settings, error) {
	c := Defaults()
	err := db.QueryRowContext(ctx, "SELECT name,description,announcement,registration,email_mode,email_domains,max_players,single_session,sign_enabled,sign_hours,sign_min,sign_max,initial_points,player_cost,oauth_enabled,upload_cost,private_upload_cost,closet_cost,skin_domains,oidc_enabled,oidc_issuer,oidc_client_id,oidc_name FROM site_settings WHERE id=1").Scan(&c.Name, &c.Description, &c.Announcement, &c.Registration, &c.EmailMode, &c.EmailDomains, &c.MaxPlayers, &c.SingleSession, &c.SignEnabled, &c.SignHours, &c.SignMin, &c.SignMax, &c.InitialPoints, &c.PlayerCost, &c.OAuthEnabled, &c.UploadCost, &c.PrivateUploadCost, &c.ClosetCost, &c.SkinDomains, &c.OIDCEnabled, &c.OIDCIssuer, &c.OIDCClientID, &c.OIDCName)
	if err != nil {
		return c, fmt.Errorf("read site settings: %w", err)
	}
	return c, nil
}

func (c Settings) Validate() error {
	if c.OIDCEnabled < 0 || c.OIDCEnabled > 1 || len(c.OIDCIssuer) > 255 || len(c.OIDCClientID) > 255 || len(c.OIDCName) > 100 {
		return ErrPolicy
	}
	if c.OIDCEnabled == 1 && (!ValidIssuer(c.OIDCIssuer) || strings.TrimSpace(c.OIDCClientID) == "" || strings.TrimSpace(c.OIDCName) == "") {
		return ErrPolicy
	}
	if strings.TrimSpace(c.Name) == "" || len(c.Name) > 100 || len(c.Description) > 1000 || len(c.Announcement) > 4000 || len(c.EmailDomains) > 2000 || len(c.SkinDomains) > 2000 || c.Registration < 0 || c.Registration > 1 || c.EmailMode < 0 || c.EmailMode > 2 || c.MaxPlayers < 1 || c.MaxPlayers > 100 || c.SingleSession < 0 || c.SingleSession > 1 || c.SignEnabled < 0 || c.SignEnabled > 1 || c.SignHours < 1 || c.SignHours > 8760 || c.SignMin < 0 || c.SignMax < c.SignMin || c.SignMax > 1000000 || c.InitialPoints < 0 || c.InitialPoints > 1000000 || c.PlayerCost < 0 || c.PlayerCost > 1000000 || c.OAuthEnabled < 0 || c.OAuthEnabled > 1 || c.UploadCost < 0 || c.UploadCost > 1000000 || c.PrivateUploadCost < c.UploadCost || c.PrivateUploadCost > 1000000 || c.ClosetCost < 0 || c.ClosetCost > 1000000 {
		return ErrPolicy
	}
	domains := strings.Fields(c.EmailDomains)
	if c.EmailMode != 0 && len(domains) == 0 {
		return ErrPolicy
	}
	for _, d := range append(domains, strings.Fields(c.SkinDomains)...) {
		if len(d) > 253 {
			return ErrPolicy
		}
		for _, label := range strings.Split(d, ".") {
			if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
				return ErrPolicy
			}
		}
		if strings.ContainsAny(d, "/@:*\\") || net.ParseIP(d) != nil || !strings.Contains(d, ".") {
			return ErrPolicy
		}
		for _, r := range d {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
				return ErrPolicy
			}
		}
	}
	return nil
}

func (c Settings) AllowsEmail(email string) bool {
	_, domain, ok := strings.Cut(email, "@")
	if !ok {
		return false
	}
	if c.EmailMode == 0 {
		return true
	}
	found := false
	for _, d := range strings.Fields(c.EmailDomains) {
		if strings.EqualFold(domain, d) {
			found = true
		}
	}
	if c.EmailMode == 1 {
		return found
	}
	return !found
}

func ValidIssuer(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "localhost" || net.ParseIP(u.Hostname()) != nil && net.ParseIP(u.Hostname()).IsLoopback())
}
