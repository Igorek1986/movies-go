package db

import (
	"context"
	"movies-api/db/postgres"
)

// Init opens the PostgreSQL connection pool and runs pending migrations.
func Init() {
	postgres.Init(context.Background())
}
