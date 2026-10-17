package migrations

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"
)

// 每个文件仅含一条 DDL；MySQL 的 DDL 隐式提交，先登记 dirty，失败后由运维核对修复。
//
//go:embed postgres/*.sql mysql/*.sql
var files embed.FS

func placeholder(dialect string, n int) string {
	if dialect == "postgres" {
		return fmt.Sprintf("$%d", n)
	}
	return "?"
}

func Check(ctx context.Context, db *sql.DB, dialect string) error {
	if db == nil {
		return fmt.Errorf("database is not configured")
	}
	entries, err := fs.Glob(files, dialect+"/[0-9]*.sql")
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("unsupported migration dialect")
	}
	rows, err := db.QueryContext(ctx, "SELECT version, checksum, dirty FROM schema_migrations ORDER BY version")
	if err != nil {
		return fmt.Errorf("schema is not initialized")
	}
	defer rows.Close()
	i := 0
	for rows.Next() {
		var version, dirty int
		var checksum string
		if err := rows.Scan(&version, &checksum, &dirty); err != nil {
			return fmt.Errorf("read schema version")
		}
		if i >= len(entries) || version != i+1 || dirty != 0 {
			return fmt.Errorf("schema migration state mismatch")
		}
		data, err := files.ReadFile(entries[i])
		if err != nil {
			return fmt.Errorf("read migration: %w", err)
		}
		if checksum != fmt.Sprintf("%x", sha256.Sum256(data)) {
			return fmt.Errorf("migration checksum mismatch")
		}
		i++
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema version")
	}
	if i != len(entries) {
		return fmt.Errorf("schema migrations are pending")
	}
	return nil
}

func Apply(ctx context.Context, db *sql.DB, dialect string) error {
	entries, err := fs.Glob(files, dialect+"/[0-9]*.sql")
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("unsupported migration dialect")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection")
	}
	defer conn.Close()
	lockSQL := "SELECT pg_advisory_lock(763508242)"
	unlockSQL := "SELECT pg_advisory_unlock(763508242)"
	metadataSQL, err := files.ReadFile(dialect + "/history.sql")
	if err != nil {
		return fmt.Errorf("read migration history schema: %w", err)
	}
	if dialect == "mysql" {
		lockSQL = "SELECT GET_LOCK('yggserver_migrations', 10)"
		unlockSQL = "SELECT RELEASE_LOCK('yggserver_migrations')"
		var locked sql.NullInt64
		if err := conn.QueryRowContext(ctx, lockSQL).Scan(&locked); err != nil || !locked.Valid || locked.Int64 != 1 {
			return fmt.Errorf("acquire migration lock")
		}
	} else {
		if _, err := conn.ExecContext(ctx, lockSQL); err != nil {
			return fmt.Errorf("acquire migration lock")
		}
	}
	defer func() {
		unlockCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.ExecContext(unlockCtx, unlockSQL); err != nil {
			// 无法释放会话锁时销毁物理连接，禁止把持锁连接放回池中。
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err := conn.ExecContext(ctx, string(metadataSQL)); err != nil {
		return fmt.Errorf("initialize migration history")
	}
	for i, name := range entries {
		data, err := files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration: %w", err)
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(data))
		var saved string
		var dirty int
		err = conn.QueryRowContext(ctx, "SELECT checksum, dirty FROM schema_migrations WHERE version = "+placeholder(dialect, 1), i+1).Scan(&saved, &dirty)
		if err == nil {
			if dirty != 0 || saved != checksum {
				return fmt.Errorf("migration %d is dirty or modified; manual repair required", i+1)
			}
			continue
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("read migration history")
		}
		if _, err := conn.ExecContext(ctx, "INSERT INTO schema_migrations (version, checksum, dirty) VALUES ("+placeholder(dialect, 1)+", "+placeholder(dialect, 2)+", 1)", i+1, checksum); err != nil {
			return fmt.Errorf("record migration %d", i+1)
		}
		if _, err := conn.ExecContext(ctx, string(data)); err != nil {
			return fmt.Errorf("apply migration %d failed; manual repair required", i+1)
		}
		if _, err := conn.ExecContext(ctx, "UPDATE schema_migrations SET dirty = 0 WHERE version = "+placeholder(dialect, 1), i+1); err != nil {
			return fmt.Errorf("complete migration %d", i+1)
		}
	}
	return nil
}
