package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ShuilingXu/YggServer-Go/internal/config"
	"github.com/ShuilingXu/YggServer-Go/internal/database"
	"github.com/ShuilingXu/YggServer-Go/internal/httpserver"
	"github.com/ShuilingXu/YggServer-Go/internal/signing"
	"github.com/ShuilingXu/YggServer-Go/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{ReplaceAttr: func(groups []string, attr slog.Attr) slog.Attr {
		if len(groups) == 0 && attr.Key == slog.TimeKey {
			attr.Value = slog.TimeValue(attr.Value.Time().UTC())
		}
		return attr
	}}))
	if err := run(logger); err != nil {
		logger.Error("startup failed", "reason", err.Error())
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	keyPath := flag.String("generate-key", "", "生成 RSA 4096 私钥到指定的新文件后退出")
	migrate := flag.Bool("migrate", false, "显式执行数据库迁移后退出")
	flag.Parse()
	if flag.NArg() != 0 || (*keyPath != "" && *migrate) {
		return fmt.Errorf("invalid command arguments")
	}
	if *keyPath != "" {
		return signing.Generate(*keyPath)
	}
	c, err := config.Load()
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var db *sql.DB
	if c.DatabaseDSN != "" || *migrate {
		connectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		db, err = database.Open(connectCtx, c.DatabaseDriver, c.DatabaseDSN)
		cancel()
		if err != nil {
			return err
		}
		defer db.Close()
	}
	if *migrate {
		migrationCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := migrations.Apply(migrationCtx, db, c.DatabaseDriver); err != nil {
			return err
		}
		if err := migrations.Check(migrationCtx, db, c.DatabaseDriver); err != nil {
			return err
		}
		logger.Info("migrations complete", "driver", c.DatabaseDriver)
		return nil
	}
	key, err := signing.Load(c.SigningKeyFile)
	if err != nil {
		return err
	}
	publicKey, err := signing.PublicPEM(key)
	if err != nil {
		return err
	}
	if c.WebDir != "" {
		if info, err := os.Stat(filepath.Join(c.WebDir, "index.html")); err != nil || info.IsDir() {
			return fmt.Errorf("YGG_WEB_DIR must contain a built index.html")
		}
	}
	listener, err := net.Listen("tcp", c.ListenAddr)
	if err != nil {
		return fmt.Errorf("listen failed")
	}
	defer listener.Close()
	if c.PublicURL == "" {
		host, port, _ := net.SplitHostPort(listener.Addr().String())
		if host == "" || host == "::" || host == "0.0.0.0" {
			host = "127.0.0.1"
		}
		c.PublicURL = "http://" + net.JoinHostPort(host, port)
	}
	handler, err := httpserver.New(c, publicKey, db, logger)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 1 << 20}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	logger.Info("server listening", "address", listener.Addr().String(), "public_url", c.PublicURL)
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server failed")
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
			<-done
			return fmt.Errorf("graceful shutdown failed")
		}
		<-done
		logger.Info("server stopped")
	}
	return nil
}
