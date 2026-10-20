package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"strings"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/redisstore"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
	"golang.org/x/crypto/bcrypt"
)

type Service struct {
	LegacySalt string
	External   *ExternalLogin
	Store      *Store
	Now        func() time.Time
	Redis      *redisstore.Store
	Recovery   *Recovery
	dummyHash  []byte
}

func New(store *Store, now func() time.Time) (*Service, error) {
	dummy, err := bcrypt.GenerateFromPassword([]byte(rand.Text()), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &Service{Store: store, Now: now, dummyHash: dummy}, nil
}

func (s *Service) Register(ctx context.Context, email, password, name string) error {
	email, err := Email(email)
	if err != nil || !ValidPassword(password) || !ValidPlayer(name) {
		return ErrInvalid
	}
	policy, err := site.Read(ctx, s.Store.DB)
	if err != nil {
		return err
	}
	if policy.Registration != 1 || !policy.AllowsEmail(email) {
		return ErrDenied
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.Store.Register(ctx, Account{UUID: model.UUID(), Email: email, PasswordHash: hash, CreatedAt: s.Now().UTC()}, name)
}

func (s *Service) Credentials(ctx context.Context, identification, password string) (Account, error) {
	if len(identification) > 255 || len(password) > 72 {
		return Account{}, ErrCredentials
	}
	a, err := s.Store.Account(ctx, strings.ToLower(strings.TrimSpace(identification)))
	if s.Redis != nil {
		key := strings.ToLower(strings.TrimSpace(identification))
		if err == nil {
			key = a.UUID
		}
		allowed, limitErr := s.Redis.Allow(ctx, "account-login:"+key, 20, time.Minute)
		if limitErr != nil {
			return Account{}, ErrUnavailable
		}
		if !allowed {
			return Account{}, ErrRateLimited
		}
	}
	hash := a.PasswordHash
	if err != nil {
		hash = s.dummyHash
	}
	passwordOK := verifyPassword(hash, password, s.LegacySalt)
	if err != nil {
		return Account{}, err
	}
	if !passwordOK || a.State != model.AccountActive {
		return Account{}, ErrCredentials
	}
	if !strings.HasPrefix(string(hash), "$2") {
		upgraded, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return Account{}, err
		}
		if err = s.Store.upgradePassword(ctx, a.ID, hash, upgraded); err != nil {
			return Account{}, err
		}
		a.PasswordHash = upgraded
	}
	return a, nil
}

func (s *Service) Authenticate(ctx context.Context, identification, password string, clientToken *string, kind model.TokenKind, requestUser bool) (LoginResponse, error) {
	client := ""
	if clientToken != nil {
		client = *clientToken
	}
	if len(client) > 256 {
		return LoginResponse{}, ErrInvalid
	}
	a, err := s.Credentials(ctx, identification, password)
	if err != nil {
		return LoginResponse{}, err
	}
	profiles, err := s.Store.Profiles(ctx, a.ID)
	if err != nil {
		return LoginResponse{}, err
	}
	selected := ""
	if kind == model.TokenYggdrasil {
		if clientToken == nil {
			client = model.UUID()
		}
		if len(profiles) == 1 {
			selected = profiles[0].UUID
		}
		for _, p := range profiles {
			if strings.EqualFold(p.Name, identification) {
				selected = p.UUID
			}
		}
	}
	return s.issue(ctx, a, client, selected, kind, profiles, "", requestUser)
}

func (s *Service) issue(ctx context.Context, a Account, client, selected string, kind model.TokenKind, profiles []Profile, oldHash string, requestUser bool) (LoginResponse, error) {
	now := s.Now().UTC()
	access := rand.Text()
	t := Token{Account: a, Hash: Hash(access), ClientHash: Hash(client), PlayerUUID: selected, Kind: kind, ExpiresAt: now.Add(24 * time.Hour), RefreshUntil: now.Add(7 * 24 * time.Hour)}
	sealed, err := sealClient(access, client)
	if err != nil {
		return LoginResponse{}, err
	}
	t.ClientCipher = sealed
	if kind == model.TokenWeb {
		t.RefreshUntil = t.ExpiresAt
	}
	if err := s.Store.Issue(ctx, t, oldHash, now); err != nil {
		return LoginResponse{}, err
	}
	response := LoginResponse{AccessToken: access, ClientToken: client, AvailableProfiles: profiles}
	for _, p := range profiles {
		if p.UUID == selected {
			profile := p
			response.SelectedProfile = &profile
		}
	}
	if requestUser {
		response.User = &struct {
			ID         string `json:"id"`
			Properties []any  `json:"properties"`
		}{a.UUID, []any{}}
	}
	return response, nil
}

func (s *Service) Validate(ctx context.Context, access string, client *string, kind model.TokenKind, refresh bool) (Token, error) {
	if access == "" || len(access) > 256 {
		return Token{}, ErrCredentials
	}
	t, err := s.Store.Token(ctx, Hash(access))
	if err != nil {
		return Token{}, err
	}
	if subtle.ConstantTimeCompare([]byte(t.Hash), []byte(Hash(access))) != 1 {
		return Token{}, ErrCredentials
	}
	return t, t.Check(s.Now(), kind, client, refresh)
}

func (s *Service) JoinedToken(ctx context.Context, hash string) (Token, error) {
	t, err := s.Store.Token(ctx, hash)
	if err != nil {
		return Token{}, err
	}
	if t.PlayerUUID == "" {
		return Token{}, ErrCredentials
	}
	return t, t.Check(s.Now(), model.TokenYggdrasil, nil, false)
}

func (s *Service) Refresh(ctx context.Context, access string, client *string, profile *Profile, requestUser bool) (LoginResponse, error) {
	t, err := s.Validate(ctx, access, client, model.TokenYggdrasil, true)
	if err != nil {
		return LoginResponse{}, err
	}
	clearClient, err := openClient(access, t.ClientCipher)
	if err != nil {
		return LoginResponse{}, err
	}
	profiles, err := s.Store.Profiles(ctx, t.Account.ID)
	if err != nil {
		return LoginResponse{}, err
	}
	selected := t.PlayerUUID
	if profile != nil {
		if selected != "" {
			return LoginResponse{}, ErrInvalid
		}
		found := false
		for _, p := range profiles {
			if p.UUID == profile.UUID && p.Name == profile.Name {
				found = true
				selected = p.UUID
			}
		}
		if !found {
			return LoginResponse{}, ErrDenied
		}
	}
	return s.issue(ctx, t.Account, clearClient, selected, t.Kind, profiles, t.Hash, requestUser)
}

func (s *Service) UpdateAccount(ctx context.Context, t Token, current, next, email string, remove bool) error {
	if !verifyPassword(t.Account.PasswordHash, current, s.LegacySalt) {
		return ErrCredentials
	}
	if remove {
		if t.Account.Permission == model.PermissionOwner {
			return ErrDenied
		}
		return s.Store.UpdateAccount(ctx, t.Account, nil, "", true)
	}
	if email != "" {
		email, err := Email(email)
		if err != nil {
			return err
		}
		policy, err := site.Read(ctx, s.Store.DB)
		if err != nil {
			return err
		}
		if !policy.AllowsEmail(email) {
			return ErrDenied
		}
		return s.Store.UpdateAccount(ctx, t.Account, nil, email, false)
	}
	if !ValidPassword(next) {
		return ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(next), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.Store.UpdateAccount(ctx, t.Account, hash, "", false)
}
