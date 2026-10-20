package site

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/ShuilingXu/YggServer-Go/internal/database"
	"time"
)

type PointKind int16

const (
	PointInitial PointKind = 0
	PointSign    PointKind = 1
	PointPlayer  PointKind = 2
	PointUpload  PointKind = 3
	PointCloset  PointKind = 4
	PointPrivacy PointKind = 5
	PointImport  PointKind = 6
)

// 调用方必须先锁定账号；积分变更与业务共用事务，不允许余额检查与扣款分离。
func Change(ctx context.Context, tx *sql.Tx, dialect string, account int64, delta int64, kind PointKind, now time.Time) error {
	var balance int64
	if err := tx.QueryRowContext(ctx, database.Bind(dialect, "SELECT COALESCE(SUM(delta),0) FROM point_entries WHERE account_id=?"), account).Scan(&balance); err != nil {
		return fmt.Errorf("read points: %w", err)
	}
	if balance+delta < 0 {
		return ErrFunds
	}
	if delta == 0 {
		return nil
	}
	_, err := tx.ExecContext(ctx, database.Bind(dialect, "INSERT INTO point_entries(account_id,delta,kind,created_at) VALUES(?,?,?,?)"), account, delta, kind, now.UTC())
	if err != nil {
		return fmt.Errorf("record points: %w", err)
	}
	return nil
}

func (c Settings) TextureCost(size int64, public int16) int64 {
	price := c.UploadCost
	if public == 0 {
		price = c.PrivateUploadCost
	}
	return ((size + 1023) / 1024) * price
}
