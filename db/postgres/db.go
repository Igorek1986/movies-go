package postgres

import (
	"context"
	_ "embed"
	"movies-api/config"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed schema.sql
var schemaSQL string

var Pool *pgxpool.Pool

// Init opens the connection pool and applies the schema (idempotent).
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
	if _, err := Pool.Exec(ctx, schemaSQL); err != nil {
		log.Fatalf("postgres: schema apply failed: %v", err)
	}
	log.Println("postgres: connected")
}
