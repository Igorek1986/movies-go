package postgres

import (
	"context"
	"embed"
	"fmt"
	"lampa-api/config"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

var Pool *pgxpool.Pool

// Init opens the connection pool and applies pending migrations.
func Init(ctx context.Context) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = config.Get().DatabaseURL
	}
	if dsn == "" {
		log.Fatalln("DATABASE_URL is not set")
	}

	var pool *pgxpool.Pool
	for {
		var err error
		pool, err = pgxpool.New(ctx, dsn)
		if err != nil {
			log.Printf("postgres: connect failed, retrying in 5s: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		if err = pool.Ping(ctx); err != nil {
			pool.Close()
			log.Printf("postgres: ping failed, retrying in 5s: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		break
	}

	Pool = pool
	if err := applyMigrations(ctx); err != nil {
		log.Fatalf("postgres: migrations failed: %v", err)
	}
	log.Println("postgres: connected and migrations applied")
}

func applyMigrations(ctx context.Context) error {
	// Ensure the migrations tracking table exists.
	_, err := Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    VARCHAR(100) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// Read applied versions.
	rows, err := Pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query schema_migrations: %w", err)
	}
	applied := map[string]bool{}
	for rows.Next() {
		var v string
		rows.Scan(&v) //nolint:errcheck
		applied[v] = true
	}
	rows.Close()

	// Collect migration files.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	for _, name := range files {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}
		sql, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err = Pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		Pool.Exec(ctx, //nolint:errcheck
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		log.Printf("postgres: applied migration %s", name)
	}
	return nil
}
