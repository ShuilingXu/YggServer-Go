package identity

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/audit"
	"github.com/ShuilingXu/YggServer-Go/internal/database"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

type Store struct {
	DB           *sql.DB
	Dialect      string
	UUIDStrategy model.UUIDStrategy
}

func (s *Store) q(query string) string { return database.Bind(s.Dialect, query) }

func storeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrCredentials
	}
	var pg *pgconn.PgError
	var my *mysql.MySQLError
	if (errors.As(err, &pg) && pg.Code == "23505") || (errors.As(err, &my) && my.Number == 1062) {
		return ErrConflict
	}
	return fmt.Errorf("identity storage: %w", err)
}

const accountColumns = "a.id,a.uuid,a.email,a.password_hash,a.state,a.permission,a.verified,a.created_at"

type scanner interface{ Scan(...any) error }

func scanAccount(row scanner) (Account, error) {
	var a Account
	err := row.Scan(&a.ID, &a.UUID, &a.Email, &a.PasswordHash, &a.State, &a.Permission, &a.Verified, &a.CreatedAt)
	return a, storeError(err)
}

func (s *Store) Account(ctx context.Context, identification string) (Account, error) {
	return scanAccount(s.DB.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.email=? OR a.id=(SELECT account_id FROM players WHERE LOWER(name)=LOWER(?))"), identification, identification))
}

func (s *Store) Register(ctx context.Context, a Account, name string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	policy, err := site.Read(ctx, tx)
	if err != nil {
		return err
	}
	if policy.Registration != 1 || !policy.AllowsEmail(a.Email) {
		return ErrDenied
	}
	if _, err := s.createAccount(ctx, tx, a, name, policy); err != nil {
		return err
	}
	return storeError(tx.Commit())
}

func (s *Store) createAccount(ctx context.Context, tx *sql.Tx, a Account, name string, policy site.Settings) (int64, error) {
	_, err := tx.ExecContext(ctx, s.q("INSERT INTO accounts(uuid,email,password_hash,created_at,verified) VALUES(?,?,?,?,?)"), a.UUID, a.Email, a.PasswordHash, a.CreatedAt, a.Verified)
	if err != nil {
		return 0, storeError(err)
	}
	var id int64
	err = tx.QueryRowContext(ctx, s.q("SELECT id FROM accounts WHERE uuid=?"), a.UUID).Scan(&id)
	if err != nil {
		return 0, storeError(err)
	}
	_, err = tx.ExecContext(ctx, s.q("INSERT INTO players(uuid,account_id,name,created_at,updated_at,uuid_strategy) VALUES(?,?,?,?,?,?)"), PlayerUUID(name, s.UUIDStrategy), id, name, a.CreatedAt, a.CreatedAt, s.UUIDStrategy)
	if err != nil {
		return 0, storeError(err)
	}
	if err := site.Change(ctx, tx, s.Dialect, id, policy.InitialPoints, site.PointInitial, a.CreatedAt); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) Profiles(ctx context.Context, accountID int64) ([]Profile, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT uuid,name FROM players WHERE account_id=? ORDER BY id"), accountID)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	profiles := []Profile{}
	for rows.Next() {
		var p Profile
		if err := rows.Scan(&p.UUID, &p.Name); err != nil {
			return nil, storeError(err)
		}
		profiles = append(profiles, p)
	}
	return profiles, storeError(rows.Err())
}

func (s *Store) Token(ctx context.Context, hash string) (Token, error) {
	var t Token
	var player sql.NullString
	err := s.DB.QueryRowContext(ctx, s.q("SELECT "+accountColumns+",t.access_hash,t.client_hash,t.player_uuid,t.kind,t.expires_at,t.refresh_until,t.client_cipher FROM tokens t JOIN accounts a ON a.id=t.account_id WHERE t.access_hash=?"), hash).Scan(&t.Account.ID, &t.Account.UUID, &t.Account.Email, &t.Account.PasswordHash, &t.Account.State, &t.Account.Permission, &t.Account.Verified, &t.Account.CreatedAt, &t.Hash, &t.ClientHash, &player, &t.Kind, &t.ExpiresAt, &t.RefreshUntil, &t.ClientCipher)
	t.PlayerUUID = player.String
	return t, storeError(err)
}

// 账号锁串行化颁发、刷新和撤销，避免改密与登录并发时产生仍可用的旧密码令牌。
func (s *Store) Issue(ctx context.Context, t Token, oldHash string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	a, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.id=? FOR UPDATE"), t.Account.ID))
	if err != nil {
		return err
	}
	if a.State != model.AccountActive || subtle.ConstantTimeCompare(a.PasswordHash, t.Account.PasswordHash) != 1 {
		return ErrCredentials
	}
	if oldHash != "" {
		result, err := tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE access_hash=? AND account_id=? AND refresh_until>?"), oldHash, a.ID, now)
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
	}
	_, err = tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE account_id=? AND refresh_until<=?"), a.ID, now)
	if err != nil {
		return storeError(err)
	}
	policy, err := site.Read(ctx, tx)
	if err != nil {
		return err
	}
	if policy.SingleSession == 1 {
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE account_id=? AND kind=?"), a.ID, t.Kind); err != nil {
			return storeError(err)
		}
	}
	var count int
	err = tx.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM tokens WHERE account_id=?"), a.ID).Scan(&count)
	if err != nil {
		return storeError(err)
	}
	if count >= 20 {
		var oldest string
		if err := tx.QueryRowContext(ctx, s.q("SELECT access_hash FROM tokens WHERE account_id=? ORDER BY created_at,id LIMIT 1"), a.ID).Scan(&oldest); err != nil {
			return storeError(err)
		}
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE access_hash=?"), oldest); err != nil {
			return storeError(err)
		}
	}
	var player any
	if t.PlayerUUID != "" {
		player = t.PlayerUUID
	}
	_, err = tx.ExecContext(ctx, s.q("INSERT INTO tokens(account_id,access_hash,client_hash,player_uuid,kind,created_at,expires_at,refresh_until,client_cipher) VALUES(?,?,?,?,?,?,?,?,?)"), a.ID, t.Hash, t.ClientHash, player, t.Kind, now, t.ExpiresAt, t.RefreshUntil, t.ClientCipher)
	if err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (s *Store) Revoke(ctx context.Context, hash string) error {
	_, err := s.DB.ExecContext(ctx, s.q("DELETE FROM tokens WHERE access_hash=?"), hash)
	return storeError(err)
}

func (s *Store) UpdateAccount(ctx context.Context, a Account, passwordHash []byte, email string, remove bool) error {
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
	if remove {
		_, err = tx.ExecContext(ctx, s.q("UPDATE accounts SET state=2 WHERE id=?"), a.ID)
		if err == nil {
			_, err = tx.ExecContext(ctx, s.q("DELETE FROM closet WHERE account_id=?"), a.ID)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, s.q("DELETE FROM players WHERE account_id=?"), a.ID)
		}
		if err == nil {
			_, err = tx.ExecContext(ctx, s.q("UPDATE textures SET is_public=0 WHERE account_id=?"), a.ID)
		}
	} else if passwordHash != nil {
		_, err = tx.ExecContext(ctx, s.q("UPDATE accounts SET password_hash=? WHERE id=?"), passwordHash, a.ID)
	} else if email != "" {
		_, err = tx.ExecContext(ctx, s.q("UPDATE accounts SET email=?,verified=0 WHERE id=?"), email, a.ID)
	}
	if err != nil {
		return storeError(err)
	}
	if remove || passwordHash != nil || email != "" {
		if _, err := tx.ExecContext(ctx, s.q("DELETE FROM recovery_tokens WHERE account_id=?"), a.ID); err != nil {
			return storeError(err)
		}
	}
	_, err = tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE account_id=?"), a.ID)
	if err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (s *Store) Signout(ctx context.Context, a Account) error {
	return s.UpdateAccount(ctx, a, nil, "", false)
}

func (s *Store) Users(ctx context.Context, offset int) ([]Account, error) {
	rows, err := s.DB.QueryContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a ORDER BY a.id LIMIT 50 OFFSET ?"), offset)
	if err != nil {
		return nil, storeError(err)
	}
	defer rows.Close()
	users := []Account{}
	for rows.Next() {
		a, err := scanAccount(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, a)
	}
	return users, storeError(rows.Err())
}

func (s *Store) Manage(ctx context.Context, actorID int64, uuid string, state model.AccountState, permission model.Permission, requestID string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	// 管理操作锁定所有特权账号，权限变更和最后一个所有者保护使用同一个事务快照。
	rows, err := tx.QueryContext(ctx, "SELECT id FROM accounts WHERE permission>0 ORDER BY id FOR UPDATE")
	if err != nil {
		return storeError(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return storeError(err)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return storeError(err)
	}
	actor, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.id=? FOR UPDATE"), actorID))
	if err != nil {
		return err
	}
	target, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.uuid=? FOR UPDATE"), uuid))
	if err != nil {
		return err
	}
	if actor.State != model.AccountActive || actor.Permission < model.PermissionAdmin || actor.ID == target.ID || target.State == model.AccountDeleted {
		return ErrDenied
	}
	if actor.Permission == model.PermissionAdmin && (target.Permission != model.PermissionUser || permission != model.PermissionUser) {
		return ErrDenied
	}
	if target.Permission == model.PermissionOwner {
		return ErrDenied
	}
	_, err = tx.ExecContext(ctx, s.q("UPDATE accounts SET state=?,permission=? WHERE id=?"), state, permission, target.ID)
	if err != nil {
		return storeError(err)
	}
	_, err = tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE account_id=?"), target.ID)
	if err != nil {
		return storeError(err)
	}
	if err := audit.Record(ctx, tx, s.Dialect, actor.UUID, target.UUID, model.AuditUserManage, requestID, now, model.AuditApplied, int16(state), int16(permission)); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (s *Store) PromoteOwner(ctx context.Context, email string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	a, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.email=? FOR UPDATE"), email))
	if err != nil {
		return err
	}
	if a.State != model.AccountActive {
		return ErrInvalid
	}
	if _, err := tx.ExecContext(ctx, s.q("UPDATE accounts SET permission=2 WHERE id=?"), a.ID); err != nil {
		return storeError(err)
	}
	if _, err := tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE account_id=?"), a.ID); err != nil {
		return storeError(err)
	}
	if err := audit.Record(ctx, tx, s.Dialect, "", a.UUID, model.AuditOwnerGrant, "cli", now, model.AuditApplied, model.AuditUnchanged, int16(model.PermissionOwner)); err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func (s *Store) SavePlayer(ctx context.Context, a Account, uuid, name string, remove bool, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storeError(err)
	}
	defer tx.Rollback()
	current, err := scanAccount(tx.QueryRowContext(ctx, s.q("SELECT "+accountColumns+" FROM accounts a WHERE a.id=? FOR UPDATE"), a.ID))
	if err != nil {
		return err
	}
	if current.State != model.AccountActive {
		return ErrDenied
	}
	if uuid == "" {
		var count int
		err = tx.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM players WHERE account_id=?"), a.ID).Scan(&count)
		if err != nil {
			return storeError(err)
		}
		policy, err := site.Read(ctx, tx)
		if err != nil {
			return err
		}
		if count >= policy.MaxPlayers {
			return ErrDenied
		}
		if err := site.Change(ctx, tx, s.Dialect, a.ID, -policy.PlayerCost, site.PointPlayer, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, s.q("INSERT INTO players(uuid,account_id,name,created_at,updated_at,uuid_strategy) VALUES(?,?,?,?,?,?)"), PlayerUUID(name, s.UUIDStrategy), a.ID, name, now, now, s.UUIDStrategy)
	} else {
		var id int64
		var strategy model.UUIDStrategy
		err = tx.QueryRowContext(ctx, s.q("SELECT id,uuid_strategy FROM players WHERE uuid=? AND account_id=? FOR UPDATE"), uuid, a.ID).Scan(&id, &strategy)
		if err != nil {
			return storeError(err)
		}
		_, err = tx.ExecContext(ctx, s.q("DELETE FROM tokens WHERE player_uuid=?"), uuid)
		if err != nil {
			return storeError(err)
		}
		if remove {
			_, err = tx.ExecContext(ctx, s.q("DELETE FROM players WHERE id=?"), id)
		} else {
			_, err = tx.ExecContext(ctx, s.q("UPDATE players SET name=?,uuid=?,updated_at=? WHERE id=?"), name, renamedUUID(name, uuid, strategy), now, id)
		}
	}
	if err != nil {
		return storeError(err)
	}
	return storeError(tx.Commit())
}

func renamedUUID(name, current string, strategy model.UUIDStrategy) string {
	if strategy == model.UUIDRandom {
		return current
	}
	return OfflineUUID(name)
}
