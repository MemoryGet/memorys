package migration

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

func TestApplyRejectsInvalidInputs(t *testing.T) {
	for name, source := range map[string][]byte{
		"empty":            nil,
		"missing envelope": []byte("CREATE TABLE example(id text);"),
		"trailing content": []byte("BEGIN; SELECT 1; COMMIT; SELECT 2;"),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Apply(context.Background(), nil, "v1", source)
			if !errors.Is(err, ErrInvalidMigration) {
				t.Fatalf("expected invalid migration, got %v", err)
			}
		})
	}
}

func TestTransactionBodyAcceptsLeadingComment(t *testing.T) {
	body, err := transactionBody("-- prototype\nBEGIN;\nSELECT 1;\nCOMMIT;")
	if err != nil || body != "SELECT 1;" {
		t.Fatalf("body=%q err=%v", body, err)
	}
}

func TestApplyRequiresDatabase(t *testing.T) {
	_, err := Apply(context.Background(), (*sql.DB)(nil), "v1", []byte("BEGIN; SELECT 1; COMMIT;"))
	if !errors.Is(err, ErrInvalidMigration) {
		t.Fatalf("expected invalid migration, got %v", err)
	}
}
