package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

func Open(ctx context.Context, dialect, dsn string) (*sql.DB, error) {
	if dsn == "" {
		return nil, fmt.Errorf("YGG_DATABASE_DSN is required")
	}
	var db *sql.DB
	switch dialect {
	case "postgres":
		cfg, err := pgx.ParseConfig(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid postgres connection configuration")
		}
		cfg.RuntimeParams["timezone"] = "UTC"
		cfg.ConnectTimeout = 5 * time.Second
		db = stdlib.OpenDB(*cfg)
	case "mysql":
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("invalid mysql connection configuration")
		}
		cfg.ParseTime = true
		cfg.Loc = time.UTC
		cfg.MultiStatements = false
		cfg.Timeout = 5 * time.Second
		cfg.ReadTimeout = 5 * time.Second
		cfg.WriteTimeout = 5 * time.Second
		if cfg.Params == nil {
			cfg.Params = map[string]string{}
		}
		cfg.Params["time_zone"] = "'+00:00'"
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			return nil, fmt.Errorf("invalid mysql connection configuration")
		}
		db = sql.OpenDB(connector)
	default:
		return nil, fmt.Errorf("unsupported database driver")
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database connection failed")
	}
	return db, nil
}
