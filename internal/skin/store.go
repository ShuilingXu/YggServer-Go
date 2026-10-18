package skin

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/database"
	"github.com/ShuilingXu/YggServer-Go/internal/model"
	"github.com/ShuilingXu/YggServer-Go/internal/site"
)

type Store struct {
	DB      *sql.DB
	Dialect string
	Quota   int64
}

func (s *Store) q(query string) string { return database.Bind(s.Dialect, query) }

func (s *Store) lockAccount(ctx context.Context, tx *sql.Tx, accountID int64) error {
	var state model.AccountState
	if err := tx.QueryRowContext(ctx, s.q("SELECT state FROM accounts WHERE id=? FOR UPDATE"), accountID).Scan(&state); err != nil {
		return storageError(err)
	}
	if state != model.AccountActive {
		return ErrDenied
	}
	return nil
}

func storageError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return ErrMissing
	}
	return fmt.Errorf("texture storage: %w", err)
}

const columns = "t.id,t.uuid,t.account_id,t.name,t.type,t.hash,t.size_bytes,t.width,t.height,t.is_public,t.created_at,t.state"

type scanner interface{ Scan(...any) error }

func scan(row scanner, extra ...any) (Texture, error) {
	var t Texture
	fields := []any{&t.ID, &t.UUID, &t.AccountID, &t.Name, &t.Type, &t.Hash, &t.Size, &t.Width, &t.Height, &t.Public, &t.CreatedAt, &t.State}
	err := row.Scan(append(fields, extra...)...)
	return t, storageError(err)
}

func (s *Store) Get(ctx context.Context, uuid string, accountID int64, includeHidden bool) (Texture, error) {
	return scan(s.DB.QueryRowContext(ctx, s.q("SELECT "+columns+" FROM textures t JOIN accounts a ON a.id=t.account_id WHERE t.uuid=? AND a.state=0 AND (t.state=0 OR (t.account_id=? AND ?=1)) AND (t.is_public=1 OR t.account_id=?)"), uuid, accountID, boolInt(includeHidden), accountID))
}

func (s *Store) List(ctx context.Context, accountID int64, mode string, options ListOptions) ([]Texture, int, error) {
	selected := columns
	if mode == "closet" {
		selected += ",c.item_name"
	}
	query := "SELECT " + selected + " FROM textures t JOIN accounts a ON a.id=t.account_id"
	args := []any{}
	switch mode {
	case "mine":
		query += " WHERE a.state=0 AND t.state=0 AND t.account_id=?"
		args = append(args, accountID)
	case "closet":
		query += " JOIN closet c ON c.texture_id=t.id WHERE a.state=0 AND t.state=0 AND c.account_id=? AND (t.is_public=1 OR t.account_id=?)"
		args = append(args, accountID, accountID)
	default:
		query += " WHERE a.state=0 AND t.state=0 AND t.is_public=1"
	}
	if options.Query != "" {
		field := "t.name"
		if mode == "closet" {
			field = "c.item_name"
		}
		query += " AND LOWER(" + field + ") LIKE ? ESCAPE '!'"
		escaped := strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(strings.ToLower(options.Query))
		args = append(args, "%"+escaped+"%")
	}
	if options.Kind != nil {
		query += " AND t.type=?"
		args = append(args, *options.Kind)
	}
	if options.Uploader != "" {
		query += " AND a.uuid=?"
		args = append(args, options.Uploader)
	}
	var total int
	from := strings.Index(query, " FROM ")
	if err := s.DB.QueryRowContext(ctx, s.q("SELECT COUNT(*)"+query[from:]), args...).Scan(&total); err != nil {
		return nil, 0, storageError(err)
	}
	order := "t.id DESC"
	switch options.Sort {
	case "oldest":
		order = "t.id ASC"
	case "name":
		order = "LOWER(t.name),t.id"
	}
	query += " ORDER BY " + order + " LIMIT ? OFFSET ?"
	args = append(args, options.Limit, options.Offset)
	rows, err := s.DB.QueryContext(ctx, s.q(query), args...)
	if err != nil {
		return nil, 0, storageError(err)
	}
	defer rows.Close()
	items := []Texture{}
	for rows.Next() {
		var name string
		var t Texture
		if mode == "closet" {
			t, err = scan(rows, &name)
			t.ItemName = name
		} else {
			t, err = scan(rows)
		}
		if err != nil {
			return nil, 0, err
		}
		items = append(items, t)
	}
	return items, total, storageError(rows.Err())
}

func (s *Store) Upload(ctx context.Context, conn *sql.Conn, t Texture, playerUUID string) error {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return storageError(err)
	}
	defer tx.Rollback()
	if err := s.lockAccount(ctx, tx, t.AccountID); err != nil {
		return err
	}
	policy, err := site.Read(ctx, tx)
	if err != nil {
		return err
	}
	if err := site.Change(ctx, tx, s.Dialect, t.AccountID, -policy.TextureCost(t.Size, t.Public), site.PointUpload, t.CreatedAt); err != nil {
		return err
	}
	if s.Quota > 0 {
		var used int64
		if err := tx.QueryRowContext(ctx, s.q("SELECT COALESCE(SUM(size_bytes),0) FROM textures WHERE account_id=?"), t.AccountID).Scan(&used); err != nil {
			return storageError(err)
		}
		if t.Size > s.Quota || used > s.Quota-t.Size {
			return ErrQuota
		}
	}
	_, err = tx.ExecContext(ctx, s.q("INSERT INTO textures(uuid,account_id,name,type,hash,size_bytes,width,height,is_public,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)"), t.UUID, t.AccountID, t.Name, t.Type, t.Hash, t.Size, t.Width, t.Height, t.Public, t.CreatedAt)
	if err != nil {
		return storageError(err)
	}
	var id int64
	err = tx.QueryRowContext(ctx, s.q("SELECT id FROM textures WHERE uuid=?"), t.UUID).Scan(&id)
	if err != nil {
		return storageError(err)
	}
	_, err = tx.ExecContext(ctx, s.q("INSERT INTO closet(account_id,texture_id,item_name,created_at) VALUES(?,?,?,?)"), t.AccountID, id, t.Name, t.CreatedAt)
	if err != nil {
		return storageError(err)
	}
	if playerUUID != "" {
		column := "skin_id"
		if t.Type == model.TextureCape {
			column = "cape_id"
		}
		result, err := tx.ExecContext(ctx, s.q("UPDATE players SET "+column+"=?,updated_at=? WHERE uuid=? AND account_id=?"), id, t.CreatedAt, playerUUID, t.AccountID)
		if err != nil {
			return storageError(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return storageError(err)
		}
		if n != 1 {
			return ErrDenied
		}
	}
	return storageError(tx.Commit())
}

func (s *Store) Edit(ctx context.Context, accountID int64, uuid, name string, kind model.TextureType, public int16, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storageError(err)
	}
	defer tx.Rollback()
	if err := s.lockAccount(ctx, tx, accountID); err != nil {
		return err
	}
	old, err := scan(tx.QueryRowContext(ctx, s.q("SELECT "+columns+" FROM textures t WHERE t.uuid=? AND t.account_id=? FOR UPDATE"), uuid, accountID))
	if err != nil {
		return err
	}
	if old.Public == 1 && public == 0 {
		policy, err := site.Read(ctx, tx)
		if err != nil {
			return err
		}
		delta := policy.TextureCost(old.Size, 0) - policy.TextureCost(old.Size, 1)
		if err := site.Change(ctx, tx, s.Dialect, accountID, -delta, site.PointPrivacy, now); err != nil {
			return err
		}
	}
	result, err := tx.ExecContext(ctx, s.q("UPDATE textures SET name=?,type=?,is_public=? WHERE uuid=? AND account_id=?"), name, kind, public, uuid, accountID)
	if err != nil {
		return storageError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return storageError(err)
	}
	if n == 0 {
		var id int64
		if err := tx.QueryRowContext(ctx, s.q("SELECT id FROM textures WHERE uuid=? AND account_id=?"), uuid, accountID).Scan(&id); err != nil {
			return storageError(err)
		}
	}
	if public == 0 {
		_, err = tx.ExecContext(ctx, s.q("DELETE FROM closet WHERE texture_id=(SELECT id FROM textures WHERE uuid=?) AND account_id<>?"), uuid, accountID)
		if err != nil {
			return storageError(err)
		}
		for _, column := range []string{"skin_id", "cape_id"} {
			_, err = tx.ExecContext(ctx, s.q("UPDATE players SET "+column+"=NULL WHERE "+column+"=(SELECT id FROM textures WHERE uuid=?) AND account_id<>?"), uuid, accountID)
			if err != nil {
				return storageError(err)
			}
		}
	}
	return storageError(tx.Commit())
}

func (s *Store) Delete(ctx context.Context, conn *sql.Conn, accountID int64, uuid, hash string) (bool, error) {
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return false, storageError(err)
	}
	defer tx.Rollback()
	if err := s.lockAccount(ctx, tx, accountID); err != nil {
		return false, err
	}
	result, err := tx.ExecContext(ctx, s.q("DELETE FROM textures WHERE uuid=? AND account_id=? AND hash=?"), uuid, accountID, hash)
	if err != nil {
		return false, storageError(err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, storageError(err)
	}
	if n != 1 {
		return false, ErrMissing
	}
	referenced, err := s.Referenced(ctx, tx, hash)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, storageError(err)
	}
	return !referenced, nil
}

type rowQueryer interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) Referenced(ctx context.Context, query rowQueryer, hash string) (bool, error) {
	var count int
	err := query.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM textures WHERE hash=?"), hash).Scan(&count)
	return count > 0, storageError(err)
}

// 同一内容文件的发布和最后引用删除共用数据库会话锁，覆盖多个应用进程。
func (s *Store) LockTexture(ctx context.Context, hash string) (*sql.Conn, func(), error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := s.DB.Conn(ctx)
	if err != nil {
		return nil, nil, storageError(err)
	}
	lock, unlock := "SELECT pg_advisory_lock($1)", "SELECT pg_advisory_unlock($1)"
	decoded, err := hex.DecodeString(hash)
	if err != nil || len(decoded) != 32 {
		conn.Close()
		return nil, nil, ErrInvalid
	}
	var id any = int64(binary.BigEndian.Uint64(decoded[:8]))
	if s.Dialect == "mysql" {
		lock = "SELECT GET_LOCK(?,5)"
		unlock = "SELECT RELEASE_LOCK(?)"
		id = hash
	}
	if s.Dialect == "mysql" {
		var result sql.NullInt64
		err = conn.QueryRowContext(ctx, lock, id).Scan(&result)
		if err == nil && (!result.Valid || result.Int64 != 1) {
			err = fmt.Errorf("texture lock unavailable")
		}
	} else {
		_, err = conn.ExecContext(ctx, lock, id)
	}
	if err != nil {
		_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		conn.Close()
		return nil, nil, storageError(err)
	}
	release := func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, unlock, id); err != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		_ = conn.Close()
	}
	return conn, release, nil
}

func (s *Store) Closet(ctx context.Context, accountID int64, uuid, name string, remove bool, now time.Time) error {
	if !remove && !validName(name) {
		return ErrInvalid
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storageError(err)
	}
	defer tx.Rollback()
	if err := s.lockAccount(ctx, tx, accountID); err != nil {
		return err
	}
	t, err := scan(tx.QueryRowContext(ctx, s.q("SELECT "+columns+" FROM textures t JOIN accounts a ON a.id=t.account_id WHERE t.uuid=? AND a.state=0 AND t.state=0 FOR UPDATE"), uuid))
	if err != nil {
		return err
	}
	if !remove && t.Public != 1 && t.AccountID != accountID {
		return ErrDenied
	}
	if !remove {
		var count int
		if err := tx.QueryRowContext(ctx, s.q("SELECT COUNT(*) FROM closet WHERE account_id=? AND texture_id=?"), accountID, t.ID).Scan(&count); err != nil {
			return storageError(err)
		}
		if count == 0 {
			policy, err := site.Read(ctx, tx)
			if err != nil {
				return err
			}
			if err := site.Change(ctx, tx, s.Dialect, accountID, -policy.ClosetCost, site.PointCloset, now); err != nil {
				return err
			}
		}
	}
	_, err = tx.ExecContext(ctx, s.q("DELETE FROM closet WHERE account_id=? AND texture_id=?"), accountID, t.ID)
	if err != nil {
		return storageError(err)
	}
	if !remove {
		_, err = tx.ExecContext(ctx, s.q("INSERT INTO closet(account_id,texture_id,item_name,created_at) VALUES(?,?,?,?)"), accountID, t.ID, name, now)
		if err != nil {
			return storageError(err)
		}
	}
	return storageError(tx.Commit())
}

func (s *Store) Bind(ctx context.Context, accountID int64, playerUUID string, skinUUID, capeUUID *string, now time.Time) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return storageError(err)
	}
	defer tx.Rollback()
	if err := s.lockAccount(ctx, tx, accountID); err != nil {
		return err
	}
	for i, uuid := range []*string{skinUUID, capeUUID} {
		if uuid == nil {
			continue
		}
		var id any
		if *uuid != "" {
			t, err := scan(tx.QueryRowContext(ctx, s.q("SELECT "+columns+" FROM textures t JOIN accounts a ON a.id=t.account_id WHERE t.uuid=? AND a.state=0 AND t.state=0 AND (t.account_id=? OR (t.is_public=1 AND EXISTS(SELECT 1 FROM closet WHERE account_id=? AND texture_id=t.id))) FOR UPDATE"), *uuid, accountID, accountID))
			if err != nil {
				return err
			}
			if (i == 1) != (t.Type == model.TextureCape) {
				return ErrInvalid
			}
			id = t.ID
		}
		column := "skin_id"
		if i == 1 {
			column = "cape_id"
		}
		result, err := tx.ExecContext(ctx, s.q("UPDATE players SET "+column+"=?,updated_at=? WHERE uuid=? AND account_id=?"), id, now, playerUUID, accountID)
		if err != nil {
			return storageError(err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return storageError(err)
		}
		if n == 0 {
			var found int
			if err := tx.QueryRowContext(ctx, s.q("SELECT 1 FROM players WHERE uuid=? AND account_id=?"), playerUUID, accountID).Scan(&found); err != nil {
				return ErrDenied
			}
		}
	}
	return storageError(tx.Commit())
}

func (s *Store) CanDistribute(ctx context.Context, hash string) (bool, error) {
	var exists int
	err := s.DB.QueryRowContext(ctx, s.q("SELECT 1 FROM textures t JOIN accounts a ON a.id=t.account_id WHERE t.hash=? AND a.state=0 AND t.state=0 AND (t.is_public=1 OR EXISTS(SELECT 1 FROM players p JOIN accounts pa ON pa.id=p.account_id WHERE pa.state=0 AND (p.skin_id=t.id OR p.cape_id=t.id))) LIMIT 1"), hash).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, storageError(err)
}

type Appearance struct {
	Profile            model.Profile
	SkinHash, CapeHash string
	Kind               model.TextureType
}

func (s *Store) Appearance(ctx context.Context, uuid string) (Appearance, error) {
	var a Appearance
	err := s.DB.QueryRowContext(ctx, s.q("SELECT p.uuid,p.name,CASE WHEN su.state=0 AND sk.state=0 THEN sk.hash ELSE '' END,CASE WHEN cu.state=0 AND ca.state=0 THEN ca.hash ELSE '' END,COALESCE(sk.type,0) FROM players p JOIN accounts u ON u.id=p.account_id LEFT JOIN textures sk ON sk.id=p.skin_id LEFT JOIN accounts su ON su.id=sk.account_id LEFT JOIN textures ca ON ca.id=p.cape_id LEFT JOIN accounts cu ON cu.id=ca.account_id WHERE p.uuid=? AND u.state=0"), uuid).Scan(&a.Profile.UUID, &a.Profile.Name, &a.SkinHash, &a.CapeHash, &a.Kind)
	return a, storageError(err)
}

func (s *Store) Lookup(ctx context.Context, names []string) ([]model.Profile, error) {
	profiles := []model.Profile{}
	for _, name := range names {
		var p model.Profile
		err := s.DB.QueryRowContext(ctx, s.q("SELECT p.uuid,p.name FROM players p JOIN accounts a ON a.id=p.account_id WHERE LOWER(p.name)=LOWER(?) AND a.state=0"), name).Scan(&p.UUID, &p.Name)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, storageError(err)
		}
		found := false
		for _, existing := range profiles {
			if existing.UUID == p.UUID {
				found = true
			}
		}
		if !found {
			profiles = append(profiles, p)
		}
	}
	return profiles, nil
}

func (s *Store) Hashes(ctx context.Context) ([]string, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT DISTINCT hash FROM textures ORDER BY hash")
	if err != nil {
		return nil, storageError(err)
	}
	defer rows.Close()
	hashes := []string{}
	for rows.Next() {
		var hash string
		if err := rows.Scan(&hash); err != nil {
			return nil, storageError(err)
		}
		hashes = append(hashes, hash)
	}
	return hashes, storageError(rows.Err())
}

func boolInt(value bool) int16 {
	if value {
		return 1
	}
	return 0
}
