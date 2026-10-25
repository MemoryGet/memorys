package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/MemoryGet/memorys/pkg/migration"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	defaultURL := os.Getenv("MEMORYS_DATABASE_URL")
	databaseURL := flag.String("database-url", defaultURL, "PostgreSQL connection URL (or MEMORYS_DATABASE_URL)")
	file := flag.String("file", "docs/architecture/sql/withdraw_contract_v1.sql", "migration SQL file")
	version := flag.String("version", "withdraw_contract_v1", "migration version")
	flag.Parse()
	if *databaseURL == "" {
		log.Fatal("database URL is required: set MEMORYS_DATABASE_URL or pass -database-url")
	}
	source, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("read migration: %v", err)
	}
	db, err := sql.Open("pgx", *databaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping database: %v", err)
	}
	result, err := migration.Apply(ctx, db, *version, source)
	if err != nil {
		log.Fatalf("apply migration: %v", err)
	}
	state := "already applied"
	if result.Applied {
		state = "applied"
	}
	fmt.Printf("migration %s: %s (sha256=%s)\n", result.Version, state, result.Checksum)
}
