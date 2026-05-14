package db

import (
	"context"
	_ "embed"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/001_init.sql
var migration001 string

//go:embed migrations/002_phase2.sql
var migration002 string

//go:embed migrations/003_ui_node.sql
var migration003 string

//go:embed migrations/004_phase3_artifacts.sql
var migration004 string

func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, err
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	for _, sql := range []string{migration001, migration002, migration003, migration004} {
		if _, err := pool.Exec(ctx, sql); err != nil {
			return err
		}
	}
	return nil
}
