package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type ExternalLogin struct {
	PublicURL string
	Secret    string
}
type externalFlow struct {
	Verifier, Nonce, Issuer, ClientID string
	AccountID                         int64
	ParentHash                        string
}
type ExternalStart struct {
	URL   string `json:"url"`
	State string `json:"state"`
}

func (s *Service) externalProvider(ctx context.Context, c site.Settings) (*oidc.Provider, oauth2.Config, error) {
	if s.External == nil || c.OIDCEnabled != 1 || !site.ValidIssuer(c.OIDCIssuer) {
		return nil, oauth2.Config{}, ErrUnavailable
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		return fmt.Errorf("provider redirects are not allowed")
	}}
	ctx = oidc.ClientContext(ctx, client)
	provider, err := oidc.NewProvider(ctx, c.OIDCIssuer)
	if err != nil {
		return nil, oauth2.Config{}, ErrUnavailable
	}
	var metadata struct {
		JWKS string `json:"jwks_uri"`
	}
	if provider.Claims(&metadata) != nil {
		return nil, oauth2.Config{}, ErrUnavailable
	}
	endpoint := provider.Endpoint()
	// 明文本机仅用于隔离联调；HTTPS issuer 不能降级到 HTTP 端点。
	for _, raw := range []string{endpoint.AuthURL, endpoint.TokenURL, metadata.JWKS} {
		u, err := url.Parse(raw)
		if err != nil || u.User != nil || u.Fragment != "" || !site.ValidIssuer(strings.Split(raw, "?")[0]) || strings.HasPrefix(c.OIDCIssuer, "https://") && u.Scheme != "https" {
			return nil, oauth2.Config{}, ErrUnavailable
		}
	}
	return provider, oauth2.Config{ClientID: c.OIDCClientID, ClientSecret: s.External.Secret, Endpoint: endpoint, RedirectURL: s.External.PublicURL + "/oidc/callback", Scopes: []string{oidc.ScopeOpenID, "email", "profile"}}, nil
}

func (s *Service) StartExternal(ctx context.Context, t *Token) (ExternalStart, error) {
	if s.Redis == nil {
		return ExternalStart{}, ErrUnavailable
	}
	c, err := site.Read(ctx, s.Store.DB)
	if err != nil {
		return ExternalStart{}, err
	}
	_, config, err := s.externalProvider(ctx, c)
	if err != nil {
		return ExternalStart{}, err
	}
	state := rand.Text()
	flow := externalFlow{Verifier: oauth2.GenerateVerifier(), Nonce: rand.Text(), Issuer: c.OIDCIssuer, ClientID: c.OIDCClientID}
	if t != nil {
		flow.AccountID = t.Account.ID
		flow.ParentHash = t.Hash
	}
	data, err := json.Marshal(flow)
	if err != nil {
		return ExternalStart{}, err
	}
	if err := s.Redis.SaveOIDC(ctx, state, string(data)); err != nil {
		return ExternalStart{}, ErrUnavailable
	}
	return ExternalStart{config.AuthCodeURL(state, oidc.Nonce(flow.Nonce), oauth2.S256ChallengeOption(flow.Verifier)), state}, nil
}

func (s *Service) CompleteExternal(ctx context.Context, state, code, rid string) (LoginResponse, error) {
	if s.Redis == nil || len(state) != 26 || code == "" || len(code) > 4096 {
		return LoginResponse{}, ErrCredentials
	}
	data, err := s.Redis.ConsumeOIDC(ctx, state)
	if err != nil {
		return LoginResponse{}, ErrUnavailable
	}
	if data == "" {
		return LoginResponse{}, ErrCredentials
	}
	var flow externalFlow
	if json.Unmarshal([]byte(data), &flow) != nil {
		return LoginResponse{}, ErrCredentials
	}
	c, err := site.Read(ctx, s.Store.DB)
	if err != nil {
		return LoginResponse{}, err
	}
	if c.OIDCIssuer != flow.Issuer || c.OIDCClientID != flow.ClientID {
		return LoginResponse{}, ErrCredentials
	}
	provider, config, err := s.externalProvider(ctx, c)
	if err != nil {
		return LoginResponse{}, err
	}
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("provider redirects are not allowed") }}
	ctx = oidc.ClientContext(ctx, client)
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(flow.Verifier))
	if err != nil {
		return LoginResponse{}, ErrCredentials
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return LoginResponse{}, ErrCredentials
	}
	id, err := provider.VerifierContext(ctx, &oidc.Config{ClientID: c.OIDCClientID, Now: s.Now}).Verify(ctx, raw)
	if err != nil || subtle.ConstantTimeCompare([]byte(id.Nonce), []byte(flow.Nonce)) != 1 || id.Subject == "" || len(id.Subject) > 255 {
		return LoginResponse{}, ErrCredentials
	}
	if id.AccessTokenHash != "" && id.VerifyAccessToken(token.AccessToken) != nil {
		return LoginResponse{}, ErrCredentials
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
	}
	if id.Claims(&claims) != nil {
		return LoginResponse{}, ErrCredentials
	}
	a, err := s.Store.ExternalAccount(ctx, flow.Issuer, id.Subject)
	if err == nil && flow.AccountID != 0 && a.ID != flow.AccountID {
		return LoginResponse{}, ErrConflict
	}
	if errors.Is(err, ErrCredentials) {
		now := s.Now().UTC()
		a = Account{ID: flow.AccountID, CreatedAt: now}
		if flow.AccountID == 0 {
			email, emailErr := Email(claims.Email)
			if emailErr != nil || !claims.Verified {
				return LoginResponse{}, ErrDenied
			}
			hash, hashErr := bcrypt.GenerateFromPassword([]byte(rand.Text()), bcrypt.DefaultCost)
			if hashErr != nil {
				return LoginResponse{}, hashErr
			}
			a.UUID = model.UUID()
			a.Email = email
			a.PasswordHash = hash
			a.Verified = 1
		}
		a, err = s.Store.ResolveExternal(ctx, a, flow.Issuer, id.Subject, flow.ParentHash, rid)
	}
	if err != nil {
		return LoginResponse{}, err
	}
	if a.State != model.AccountActive {
		return LoginResponse{}, ErrDenied
	}
	profiles, err := s.Store.Profiles(ctx, a.ID)
	if err != nil {
		return LoginResponse{}, err
	}
	return s.issue(ctx, a, "", "", model.TokenWeb, profiles, "", false)
}
