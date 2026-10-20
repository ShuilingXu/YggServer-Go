package identity

import (
	"context"
	"github.com/ShuilingXu/YggServer-Go/internal/audit"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
	"golang.org/x/crypto/bcrypt"
)

func (s *Service) Initialize(ctx context.Context, name, email, password, player, requestID string) error {
	email, err := Email(email)
	policy := site.Defaults()
	policy.Name = name
	if err != nil || !ValidPassword(password) || !ValidPlayer(player) || policy.Validate() != nil {
		return ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return s.Store.initialize(ctx, policy, Account{UUID: model.UUID(), Email: email, PasswordHash: hash, CreatedAt: s.Now().UTC()}, player, requestID)
}

func (s *Store) initialize(ctx context.Context, policy site.Settings, a Account, player, requestID string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	var lock int
	if err = tx.QueryRowContext(ctx, "SELECT id FROM site_settings WHERE id=1 FOR UPDATE").Scan(&lock); err != nil {
		return storeError(err)
	}
	empty, err := Uninitialized(ctx, tx)
	if err != nil {
		return storeError(err)
	}
	if !empty {
		return ErrConflict
	}
	id, err := s.createAccount(ctx, tx, a, player, policy)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, s.q("UPDATE accounts SET permission=? WHERE id=?"), model.PermissionOwner, id); err != nil {
		return storeError(err)
	}
	if err = site.Write(ctx, tx, s.Dialect, policy); err != nil {
		return err
	}
	if err = audit.Record(ctx, tx, s.Dialect, a.UUID, a.UUID, model.AuditSetup, requestID, a.CreatedAt, model.AuditApplied, -1, 2); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func Uninitialized(ctx context.Context, db site.Query) (bool, error) {
	var count int
	err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM accounts").Scan(&count)
	return count == 0, err
}

func (s *Store) upgradePassword(ctx context.Context, id int64, hash, upgraded []byte) error {
	result, err := s.DB.ExecContext(ctx, s.q("UPDATE accounts SET password_hash=? WHERE id=? AND password_hash=? AND state=0"), upgraded, id, hash)
	if err != nil {
		return storeError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storeError(err)
	}
	if n != 1 {
		return ErrCredentials
	}
	return nil
}
