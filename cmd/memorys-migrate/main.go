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
	withExtensions := flag.Bool("with-grant-management", false, "also apply access_grant_management_v1 after the selected migration")
	withFeedbackProposals := flag.Bool("with-feedback-proposals", false, "also apply feedback_proposals_v1 after the selected migration")
	withDocumentMetadata := flag.Bool("with-document-metadata", false, "also apply document_metadata_v1 after the selected migration")
	withKnowledgeBaseManagement := flag.Bool("with-knowledge-base-management", false, "also apply knowledge_base_management_v1 after the selected migration")
	withParsePause := flag.Bool("with-parse-pause", false, "also apply parse_tasks_v3 after the selected migration")
	withPublicationPause := flag.Bool("with-publication-pause", false, "also apply publication_tasks_v2 after the selected migration")
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
	if *withExtensions {
		extensionFile := "docs/architecture/sql/access_grant_management_v1.sql"
		extensionVersion := "access_grant_management_v1"
		extensionSource, readErr := os.ReadFile(extensionFile)
		if readErr != nil {
			log.Fatalf("read grant management migration: %v", readErr)
		}
		extension, applyErr := migration.Apply(ctx, db, extensionVersion, extensionSource)
		if applyErr != nil {
			log.Fatalf("apply grant management migration: %v", applyErr)
		}
		extensionState := "already applied"
		if extension.Applied {
			extensionState = "applied"
		}
		fmt.Printf("migration %s: %s (sha256=%s)\n", extension.Version, extensionState, extension.Checksum)
	}
	if *withFeedbackProposals {
		extensionFile := "docs/architecture/sql/feedback_proposals_v1.sql"
		extensionVersion := "feedback_proposals_v1"
		extensionSource, readErr := os.ReadFile(extensionFile)
		if readErr != nil {
			log.Fatalf("read feedback proposal migration: %v", readErr)
		}
		extension, applyErr := migration.Apply(ctx, db, extensionVersion, extensionSource)
		if applyErr != nil {
			log.Fatalf("apply feedback proposal migration: %v", applyErr)
		}
		extensionState := "already applied"
		if extension.Applied {
			extensionState = "applied"
		}
		fmt.Printf("migration %s: %s (sha256=%s)\n", extension.Version, extensionState, extension.Checksum)
	}
	if *withDocumentMetadata {
		extensionFile := "docs/architecture/sql/document_metadata_v1.sql"
		extensionVersion := "document_metadata_v1"
		extensionSource, readErr := os.ReadFile(extensionFile)
		if readErr != nil {
			log.Fatalf("read document metadata migration: %v", readErr)
		}
		extension, applyErr := migration.Apply(ctx, db, extensionVersion, extensionSource)
		if applyErr != nil {
			log.Fatalf("apply document metadata migration: %v", applyErr)
		}
		extensionState := "already applied"
		if extension.Applied {
			extensionState = "applied"
		}
		fmt.Printf("migration %s: %s (sha256=%s)\n", extension.Version, extensionState, extension.Checksum)
	}
	if *withKnowledgeBaseManagement {
		extensionFile := "docs/architecture/sql/knowledge_base_management_v1.sql"
		extensionVersion := "knowledge_base_management_v1"
		extensionSource, readErr := os.ReadFile(extensionFile)
		if readErr != nil {
			log.Fatalf("read knowledge base management migration: %v", readErr)
		}
		extension, applyErr := migration.Apply(ctx, db, extensionVersion, extensionSource)
		if applyErr != nil {
			log.Fatalf("apply knowledge base management migration: %v", applyErr)
		}
		extensionState := "already applied"
		if extension.Applied {
			extensionState = "applied"
		}
		fmt.Printf("migration %s: %s (sha256=%s)\n", extension.Version, extensionState, extension.Checksum)
	}
	if *withParsePause {
		extensionFile := "docs/architecture/sql/parse_tasks_v3.sql"
		extensionVersion := "parse_tasks_v3"
		extensionSource, readErr := os.ReadFile(extensionFile)
		if readErr != nil {
			log.Fatalf("read parse pause migration: %v", readErr)
		}
		extension, applyErr := migration.Apply(ctx, db, extensionVersion, extensionSource)
		if applyErr != nil {
			log.Fatalf("apply parse pause migration: %v", applyErr)
		}
		extensionState := "already applied"
		if extension.Applied {
			extensionState = "applied"
		}
		fmt.Printf("migration %s: %s (sha256=%s)\n", extension.Version, extensionState, extension.Checksum)
	}
	if *withPublicationPause {
		extensionFile := "docs/architecture/sql/publication_tasks_v2.sql"
		extensionVersion := "publication_tasks_v2"
		extensionSource, readErr := os.ReadFile(extensionFile)
		if readErr != nil {
			log.Fatalf("read publication pause migration: %v", readErr)
		}
		extension, applyErr := migration.Apply(ctx, db, extensionVersion, extensionSource)
		if applyErr != nil {
			log.Fatalf("apply publication pause migration: %v", applyErr)
		}
		extensionState := "already applied"
		if extension.Applied {
			extensionState = "applied"
		}
		fmt.Printf("migration %s: %s (sha256=%s)\n", extension.Version, extensionState, extension.Checksum)
	}
}
