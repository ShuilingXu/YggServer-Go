package identity

import (
	"context"
	"crypto/subtle"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/model"
)

func (s *Store) SaveRecovery(ctx context.Context, a Account, hash string, kind model.RecoveryKind, now, expires time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	current, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.id=? FOR UPDATE"), a.ID))
	if err != nil {
		return err
	}
	if current.State != model.AccountActive || current.Email != a.Email || subtle.ConstantTimeCompare(current.PasswordHash, a.PasswordHash) != 1 {
		return ErrCredentials
	}
	if _, err = tx.ExecContext(ctx, s.q("DELETE FROM recovery_tokens WHERE account_id=? AND (kind=? OR expires_at<=?)"), a.ID, kind, now); err != nil {
		return storeError(err)
	}
	if _, err = tx.ExecContext(ctx, s.q("INSERT INTO recovery_tokens(account_id,token_hash,email,kind,created_at,expires_at) VALUES(?,?,?,?,?,?)"), a.ID, hash, a.Email, kind, now, expires); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (s *Store) RemoveRecovery(ctx context.Context, hash string) error {
	_, err := s.DB.ExecContext(ctx, s.q("DELETE FROM recovery_tokens WHERE token_hash=?"), hash)
	return storeError(err)
}

func (s *Store) CompleteRecovery(ctx context.Context, hash string, password []byte, kind model.RecoveryKind, now time.Time) error {
	// 先定位账号，再按其他凭据操作相同的顺序锁账号，最后锁恢复凭据。
	var accountID int64
	if err := s.DB.QueryRowContext(ctx, s.q("SELECT account_id FROM recovery_tokens WHERE token_hash=?"), hash).Scan(&accountID); err != nil {
		return storeError(err)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	a, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.id=? FOR UPDATE"), accountID))
	if err != nil {
		return err
	}
	var email string
	var storedKind model.RecoveryKind
	var expires time.Time
	if err = tx.QueryRowContext(ctx, s.q("SELECT email,kind,expires_at FROM recovery_tokens WHERE token_hash=? AND account_id=? FOR UPDATE"), hash, accountID).Scan(&email, &storedKind, &expires); err != nil {
		return storeError(err)
	}
	if a.State != model.AccountActive || email != a.Email || storedKind != kind || !now.Before(expires) {
		return ErrCredentials
	}
	if kind == model.RecoveryVerify {
		_, err = tx.ExecContext(ctx, s.q("UPDATE accounts SET verified=1 WHERE id=?"), accountID)
	} else {
		_, err = tx.ExecContext(ctx, s.q("UPDATE accounts SET password_hash=? WHERE id=?"), password, accountID)
		if err == nil {
			_, err = tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE account_id=?"), accountID)
		}
	}
	if err != nil {
		return storeError(err)
	}
	query := "DELETE FROM recovery_tokens WHERE token_hash=?"
	var arg any = hash
	if kind == model.RecoveryReset {
		query = "DELETE FROM recovery_tokens WHERE account_id=?"
		arg = accountID
	}
	if _, err = tx.ExecContext(ctx, s.q(query), arg); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}
