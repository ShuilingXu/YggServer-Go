package identity

import (
	"context"
	"crypto/subtle"
	"github.com/ShuilingXu/YggServer-Go/internal/audit"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
	"time"
)

type ExternalIdentity struct {
	ID        string    `json:"id"`
	Issuer    string    `json:"issuer"`
	CreatedAt time.Time `json:"createdAt"`
}

func (s *Store) ExternalIdentities(ctx context.Context, id int64) ([]ExternalIdentity, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT uuid,issuer,created_at FROM external_identities WHERE account_id=? ORDER BY id"), id)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	items := []ExternalIdentity{}
	for rows.Next() {
		var item ExternalIdentity
		if err := rows.Scan(&item.ID, &item.Issuer, &item.CreatedAt); err != nil {
			return nil, storeError(err)
		}
		items = append(items, item)
	}
	return items, storeError(rows.Err())
}

func (s *Store) ExternalAccount(ctx context.Context, issuer, subject string) (Account, error) {
	return scanAccount(s.DB.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a JOIN external_identities e ON e.account_id=a.id WHERE e.issuer=? AND e.subject=?"), issuer, subject))
}

func (s *Store) ResolveExternal(ctx context.Context, a Account, issuer, subject, parent string, rid string) (Account, error) {
	now := a.CreatedAt
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Account{}, storeError(err)
	}
	defer tx.Rollback()
	if parent != "" {
		current, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.id=? FOR UPDATE"), a.ID))
		if err != nil {
			return Account{}, err
		}
		if current.State != model.AccountActive {
			return Account{}, ErrDenied
		}
		var hash string
		if err := tx.QueryRowContext(ctx, s.q("SELECT access_hash FROM tokens WHERE access_hash=? AND account_id=? AND kind=0 AND expires_at>? FOR UPDATE"), parent, a.ID, a.CreatedAt).Scan(&hash); err != nil {
			return Account{}, storeError(err)
		}
		a = current
	} else {
		policy, err := site.Read(ctx, tx)
		if err != nil {
			return Account{}, err
		}
		if policy.Registration != 1 || !policy.AllowsEmail(a.Email) {
			return Account{}, ErrDenied
		}
		// 新外部身份不按邮箱合并；邮箱冲突由数据库拒绝，需先登录旧账号再显式绑定。
		id, err := s.createAccount(ctx, tx, a, "OIDC_"+a.UUID[:8], policy)
		if err != nil {
			return Account{}, err
		}
		a.ID = id
	}
	uuid := model.UUID()
	_, err = tx.ExecContext(ctx, s.q("INSERT INTO external_identities(uuid,account_id,issuer,subject,created_at) VALUES(?,?,?,?,?)"), uuid, a.ID, issuer, subject, now)
	if err != nil {
		return Account{}, storeError(err)
	}
	if err := audit.Record(ctx, tx, s.Dialect, a.UUID, uuid, model.AuditExternalBind, rid, now, model.AuditApplied, -1, -1); err != nil {
		return Account{}, storeError(err)
	}
	return a, storeError(tx.Commit())
}

func (s *Store) RemoveExternal(ctx context.Context, a Account, uuid, rid string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	current, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.id=? FOR UPDATE"), a.ID))
	if err != nil {
		return err
	}
	if current.State != model.AccountActive || subtle.ConstantTimeCompare(a.PasswordHash, current.PasswordHash) != 1 {
		return ErrCredentials
	}
	result, err := tx.ExecContext(ctx, s.q("DELETE FROM external_identities WHERE uuid=? AND account_id=?"), uuid, a.ID)
	if err != nil {
		return storeError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storeError(err)
	}
	if n != 1 {
		return ErrDenied
	}
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE account_id=?"), a.ID); err != nil {
		return storeError(err)
	}
	if err := audit.Record(ctx, tx, s.Dialect, a.UUID, uuid, model.AuditExternalUnbind, rid, now, model.AuditApplied, -1, -1); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}
