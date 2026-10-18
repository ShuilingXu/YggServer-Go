package identity

import (
	"context"
	"crypto/rand"
	"errors"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"golang.org/x/crypto/bcrypt"
)

type Recovery struct {
	Send                      func(context.Context, string, string, string) error
	PublicURL                 string
	VerificationTTL, ResetTTL time.Duration
}

func (s *Service) RequestRecovery(ctx context.Context, email string, kind model.RecoveryKind) error {
	if s.Recovery == nil || s.Redis == nil {
		return ErrUnavailable
	}
	email, err := Email(email)
	if err != nil {
		return err
	}
	allowed, err := s.Redis.Allow(ctx, "recovery-send:"+Hash(email), 1, time.Minute)
	if err != nil {
		return ErrUnavailable
	}
	if !allowed {
		return nil
	}
	a, err := s.Store.Account(ctx, email)
	if errors.Is(err, ErrCredentials) {
		return nil
	}
	if err != nil {
		return err
	}
	if a.State != model.AccountActive || a.Email != email || (kind == model.RecoveryVerify && a.Verified == 1) {
		return nil
	}
	token := rand.Text()
	ttl := s.Recovery.VerificationTTL
	purpose := "verify-email"
	subject := "Verify your email"
	if kind == model.RecoveryReset {
		ttl = s.Recovery.ResetTTL
		purpose = "reset-password"
		subject = "Reset your password"
	}
	now := s.Now().UTC()
	if err := s.Store.SaveRecovery(ctx, a, Hash(token), kind, now, now.Add(ttl)); err != nil {
		return err
	}
	// 凭据放在 fragment，浏览器请求路径和反向代理日志不携带凭据。
	body := "Open the account recovery page:\n" + s.Recovery.PublicURL + "/account-recovery#action=" + purpose + "&token=" + token + "\nThis link can be used once. Ignore this message if you did not request it."
	if err := s.Recovery.Send(ctx, email, subject, body); err != nil {
		_ = s.Store.RemoveRecovery(ctx, Hash(token))
		return ErrMail
	}
	return nil
}

func (s *Service) CompleteRecovery(ctx context.Context, raw, password string, kind model.RecoveryKind) error {
	if len(raw) < 26 || len(raw) > 256 {
		return ErrCredentials
	}
	var hash []byte
	if kind == model.RecoveryReset {
		if !ValidPassword(password) {
			return ErrInvalid
		}
		var err error
		hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return err
		}
	}
	return s.Store.CompleteRecovery(ctx, Hash(raw), hash, kind, s.Now().UTC())
}
