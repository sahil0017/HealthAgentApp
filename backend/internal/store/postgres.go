package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct{ Pool *pgxpool.Pool }

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Connect(ctx context.Context, url string) (*Store, error) {
	if url == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	config, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, err
	}
	maxConnections := int32(15)
	if raw := os.Getenv("DB_MAX_CONNS"); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 32)
		if err != nil || parsed < 2 || parsed > 100 {
			return nil, fmt.Errorf("DB_MAX_CONNS must be between 2 and 100")
		}
		maxConnections = int32(parsed)
	}
	config.MaxConns = maxConnections
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 15 * time.Minute
	config.HealthCheckPeriod = 30 * time.Second
	p, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, err
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := p.Ping(pingCtx); err != nil {
		p.Close()
		return nil, err
	}
	store := &Store{Pool: p}
	if err := store.Migrate(ctx); err != nil {
		p.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() { s.Pool.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	initTx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	if _, err := initTx.Exec(ctx, `SELECT pg_advisory_xact_lock(734920184)`); err != nil {
		_ = initTx.Rollback(ctx)
		return err
	}
	if _, err := initTx.Exec(ctx, `CREATE TABLE IF NOT EXISTS cortex_schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		_ = initTx.Rollback(ctx)
		return fmt.Errorf("create migration ledger: %w", err)
	}
	if err := initTx.Commit(ctx); err != nil {
		return err
	}
	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		sql, err := migrationFiles.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return err
		}
		tx, err := s.Pool.Begin(ctx)
		if err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(734920184)`); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		var applied bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cortex_schema_migrations WHERE version=$1)`, entry.Name()).Scan(&applied); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if applied {
			_ = tx.Rollback(ctx)
			continue
		}
		if _, err = tx.Exec(ctx, string(sql)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.Exec(ctx, `INSERT INTO cortex_schema_migrations(version) VALUES($1)`, entry.Name()); err != nil {
			_ = tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}
	return nil
}
