package main

import (
	"database/sql"
	"errors"
	"os"
	"strconv"
)

// A fixture may select a local port; hosts, identities and SSL mode stay fixed.
// This is never a production connection configuration loader.
func openFixtureDB() (*sql.DB, error) {
	port := os.Getenv("AKOS_TEST_PG_PORT")
	if port == "" {
		port = "5432"
	}
	value, err := strconv.ParseUint(port, 10, 16)
	if err != nil || value < 1024 || strconv.FormatUint(value, 10) != port {
		return nil, errors.New("invalid fixture port")
	}
	return sql.Open("pgx", "postgres://postgres@127.0.0.1:"+port+"/postgres?sslmode=disable")
}
