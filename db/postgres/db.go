package postgres

import (
	"context"
	"lampa-api/config"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var Pool *pgxpool.Pool

// ensureGoManagedTables creates tables owned by the Go service (not managed by SQLAlchemy).
func ensureGoManagedTables(ctx context.Context) {
	_, err := Pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS torrents (
			hash        TEXT PRIMARY KEY,
			tmdb_id     BIGINT,
			media_type  TEXT
		)`)
	if err != nil {
		log.Printf("postgres: create torrent_cache: %v", err)
	}
}

// Init opens the connection pool, retrying until the DB (and its schema) are ready.
// Schema is managed by the Python FastAPI service (SQLAlchemy create_all).
func Init(ctx context.Context) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = config.Get().DatabaseURL
	}
	if dsn == "" {
		log.Fatalln("DATABASE_URL is not set and database_url missing from config.yml")
	}

	for {
		pool, err := pgxpool.New(ctx, dsn)
		if err != nil {
			log.Printf("postgres: connect failed, retrying in 5s: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		// Verify that the schema is ready (torrents table must exist).
		var exists bool
		err = pool.QueryRow(ctx,
			`SELECT EXISTS (
				SELECT 1 FROM information_schema.tables
				WHERE table_name = 'torrents'
			)`).Scan(&exists)
		if err != nil || !exists {
			pool.Close()
			log.Println("postgres: schema not ready yet, retrying in 5s...")
			time.Sleep(5 * time.Second)
			continue
		}
		Pool = pool
		ensureGoManagedTables(ctx)
		log.Println("postgres: connected and schema ready")
		return
	}
}
