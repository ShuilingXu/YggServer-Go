package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/audit"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

type OAuthClient struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	RedirectURI string `json:"redirectUri"`
}
type OAuthRequest struct {
	ClientID     string `json:"client_id"`
	RedirectURI  string `json:"redirect_uri"`
	ResponseType string `json:"response_type"`
	Scope        string `json:"scope"`
	State        string `json:"state"`
	Challenge    string `json:"code_challenge"`
	Method       string `json:"code_challenge_method"`
	Approve      bool   `json:"approve"`
}

var pkceVerifier = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)
var pkceChallenge = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func validRedirect(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(raw) > 2048 {
		return false
	}
	return u.Scheme == "https" || u.Scheme == "http" && (u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1")
}

type OAuthScope int16

const (
	OAuthUserRead   OAuthScope = 1
	OAuthPlayerRead OAuthScope = 2
)

var oauthScopes = []struct {
	bit  OAuthScope
	name string
}{{OAuthUserRead, "User.Read"}, {OAuthPlayerRead, "Player.Read"}}

func parseScope(raw string) (OAuthScope, error) {
	fields := strings.Fields(raw)
	if len(fields) == 0 || len(raw) > 100 {
		return 0, ErrInvalid
	}
	var result OAuthScope
	for _, name := range fields {
		var bit OAuthScope
		for _, scope := range oauthScopes {
			if name == scope.name {
				bit = scope.bit
				break
			}
		}
		if bit == 0 {
			return 0, ErrInvalid
		}
		result |= bit
	}
	return result, nil
}
func (scope OAuthScope) String() string {
	names := []string{}
	for _, entry := range oauthScopes {
		if scope&entry.bit != 0 {
			names = append(names, entry.name)
		}
	}
	return strings.Join(names, " ")
}
func (s *Store) OAuthClient(ctx context.Context, id string) (OAuthClient, error) {
	var c OAuthClient
	err := s.DB.QueryRowContext(ctx, s.q("SELECT uuid,name,redirect_uri FROM oauth_clients WHERE uuid=?"), id).Scan(&c.ID, &c.Name, &c.RedirectURI)
	return c, storeError(err)
}
func (s *Store) OAuthRequest(ctx context.Context, q OAuthRequest) (OAuthClient, OAuthScope, error) {
	c, err := s.OAuthClient(ctx, q.ClientID)
	if err != nil {
		return c, 0, err
	}
	scope, err := parseScope(q.Scope)
	if err != nil || c.RedirectURI != q.RedirectURI || q.ResponseType != "code" || q.Method != "S256" || !pkceChallenge.MatchString(q.Challenge) || len(q.State) > 512 {
		return c, 0, ErrInvalid
	}
	return c, scope, nil
}
func (s *Store) ManageClient(ctx context.Context, a Account, c OAuthClient, remove bool, rid string, now time.Time) error {
	if !remove && (strings.TrimSpace(c.Name) == "" || len(c.Name) > 100 || !validRedirect(c.RedirectURI)) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	actor, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.id=? FOR UPDATE"), a.ID))
	if err != nil {
		return err
	}
	if actor.Permission != model.PermissionOwner || actor.State != model.AccountActive {
		return ErrDenied
	}
	if remove {
		_, err = tx.ExecContext(ctx, s.q("DELETE FROM oauth_clients WHERE uuid=?"), c.ID)
	} else {
		_, err = tx.ExecContext(ctx, s.q("INSERT INTO oauth_clients(uuid,name,redirect_uri) VALUES(?,?,?)"), c.ID, c.Name, c.RedirectURI)
	}
	if err != nil {
		return storeError(err)
	}
	if err := audit.Record(ctx, tx, s.Dialect, a.UUID, c.ID, model.AuditOAuthClient, rid, now, model.AuditApplied, -1, -1); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (s *Store) OAuthClients(ctx context.Context, account int64, now time.Time) ([]OAuthClient, error) {
	query := "SELECT uuid,name,redirect_uri FROM oauth_clients ORDER BY id"
	args := []any{}
	if account != 0 {
		query = "SELECT DISTINCT c.uuid,c.name,c.redirect_uri FROM oauth_grants g JOIN oauth_clients c ON c.uuid=g.client_uuid JOIN tokens t ON t.access_hash=g.parent_hash WHERE g.account_id=? AND g.expires_at>? AND t.expires_at>?"
		args = []any{account, now.UTC(), now.UTC()}
	}
	rows, err := s.DB.QueryContext(ctx, s.q(query), args...)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	clients := []OAuthClient{}
	for rows.Next() {
		var c OAuthClient
		if err := rows.Scan(&c.ID, &c.Name, &c.RedirectURI); err != nil {
			return nil, storeError(err)
		}
		clients = append(clients, c)
	}
	return clients, storeError(rows.Err())
}

func (s *Store) RevokeOAuth(ctx context.Context, account int64, client string) error {
	_, err := s.DB.ExecContext(ctx, s.q("DELETE FROM oauth_grants WHERE account_id=? AND client_uuid=?"), account, client)
	return storeError(err)
}

func (s *Store) AuthorizeOAuth(ctx context.Context, t Token, q OAuthRequest, now time.Time) (string, error) {
	c, scope, err := s.OAuthRequest(ctx, q)
	if err != nil {
		return "", err
	}
	u, _ := url.Parse(c.RedirectURI)
	v := u.Query()
	v.Set("state", q.State)
	if !q.Approve {
		v.Set("error", "access_denied")
	} else {
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return "", storeError(err)
		}
		defer tx.Rollback()
		var id int64
		if err := tx.QueryRowContext(ctx, s.q("SELECT id FROM accounts WHERE id=? AND state=0 FOR UPDATE"), t.Account.ID).Scan(&id); err != nil {
			return "", storeError(err)
		}
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM oauth_grants WHERE account_id=? AND expires_at<=?"), id, now.UTC()); err != nil {
			return "", storeError(err)
		}
		var count int
		if err := tx.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM oauth_grants WHERE account_id=?"), id).Scan(&count); err != nil {
			return "", storeError(err)
		}
		if count >= 50 {
			return "", ErrRateLimited
		}
		code := rand.Text()
		expires := now.UTC().Add(5 * time.Minute)
		if t.ExpiresAt.Before(expires) {
			expires = t.ExpiresAt
		}
		result, err := tx.ExecContext(ctx, s.q("INSERT INTO oauth_grants(client_uuid,account_id,parent_hash,code_hash,challenge,scope,expires_at) SELECT ?,account_id,access_hash,?,?,?,? FROM tokens WHERE access_hash=? AND expires_at>?"), c.ID, Hash(code), q.Challenge, scope, expires, t.Hash, now.UTC())
		if err != nil {
			return "", storeError(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return "", storeError(err)
		}
		if n != 1 {
			return "", ErrCredentials
		}
		if err := tx.Commit(); err != nil {
			return "", storeError(err)
		}
		v.Set("code", code)
	}
	u.RawQuery = v.Encode()
	return u.String(), nil
}

type OAuthAccess struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
	Scope       string `json:"scope"`
}

func (s *Store) ExchangeOAuth(ctx context.Context, code, client, redirect, verifier string, now time.Time) (OAuthAccess, error) {
	if !pkceVerifier.MatchString(verifier) {
		return OAuthAccess{}, ErrCredentials
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return OAuthAccess{}, storeError(err)
	}
	defer tx.Rollback()
	var id int64
	var challenge string
	var scope OAuthScope
	var expires time.Time
	err = tx.QueryRowContext(ctx, s.q("SELECT g.id,g.challenge,g.scope,g.expires_at FROM oauth_grants g JOIN oauth_clients c ON c.uuid=g.client_uuid JOIN tokens t ON t.access_hash=g.parent_hash JOIN accounts a ON a.id=g.account_id WHERE g.code_hash=? AND g.access_hash IS NULL AND g.client_uuid=? AND c.redirect_uri=? AND a.state=0 AND t.expires_at>? FOR UPDATE"), Hash(code), client, redirect, now.UTC()).Scan(&id, &challenge, &scope, &expires)
	if err != nil {
		return OAuthAccess{}, storeError(err)
	}
	sum := sha256.Sum256([]byte(verifier))
	actual := base64.RawURLEncoding.EncodeToString(sum[:])
	if !expires.After(now) || subtle.ConstantTimeCompare([]byte(challenge), []byte(actual)) != 1 {
		return OAuthAccess{}, ErrCredentials
	}
	access := rand.Text()
	_, err = tx.ExecContext(ctx, s.q("UPDATE oauth_grants SET access_hash=?,expires_at=? WHERE id=? AND access_hash IS NULL"), Hash(access), now.UTC().Add(time.Hour), id)
	if err != nil {
		return OAuthAccess{}, storeError(err)
	}
	if err := tx.Commit(); err != nil {
		return OAuthAccess{}, storeError(err)
	}
	return OAuthAccess{access, "Bearer", 3600, scope.String()}, nil
}

func (s *Store) OAuthIdentity(ctx context.Context, access string, required OAuthScope, now time.Time) (int64, string, error) {
	var id int64
	var scope OAuthScope
	var uuid string
	err := s.DB.QueryRowContext(ctx, s.q("SELECT g.account_id,g.scope,a.uuid FROM oauth_grants g JOIN tokens t ON t.access_hash=g.parent_hash JOIN accounts a ON a.id=g.account_id WHERE g.access_hash=? AND g.expires_at>? AND t.expires_at>? AND a.state=0"), Hash(access), now.UTC(), now.UTC()).Scan(&id, &scope, &uuid)
	if err != nil {
		return 0, "", storeError(err)
	}
	if scope&required != required {
		return 0, "", ErrDenied
	}
	return id, uuid, nil
}
