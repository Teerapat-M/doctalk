package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestSQLStatementsIgnoresSemicolonsInLineComments(t *testing.T) {
	got := sqlStatements(`
-- free-tier Postgres; changing the dim later needs a new migration
CREATE EXTENSION IF NOT EXISTS vector;
CREATE TABLE t (id int);
`)
	if len(got) != 2 {
		t.Fatalf("len=%d got=%q", len(got), got)
	}
	if !strings.HasPrefix(got[0], "CREATE EXTENSION") {
		t.Fatalf("first = %q", got[0])
	}
	if !strings.HasPrefix(got[1], "CREATE TABLE") {
		t.Fatalf("second = %q", got[1])
	}
}

func TestInitMigrationStatementsAreSQL(t *testing.T) {
	body, err := migrationFiles.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	stmts := sqlStatements(string(body))
	if len(stmts) == 0 {
		t.Fatal("no statements")
	}
	for _, stmt := range stmts {
		if !strings.HasPrefix(strings.ToUpper(stmt), "CREATE ") {
			t.Fatalf("unexpected statement %q", stmt)
		}
	}
}

func TestEmbeddingDimMatchesMigration(t *testing.T) {
	body, err := migrationFiles.ReadFile("migrations/0001_init.sql")
	if err != nil {
		t.Fatal(err)
	}
	needle := fmt.Sprintf("vector(%d)", EmbeddingDim)
	if !strings.Contains(string(body), needle) {
		t.Fatalf("migrations/0001_init.sql should declare %s", needle)
	}
}

func TestMigrateCreatesDocumentsChunksAndVectorDim(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("ping: %v", err)
	}
	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var ext string
	if err := db.QueryRowContext(ctx, `SELECT extname FROM pg_extension WHERE extname = 'vector'`).Scan(&ext); err != nil {
		t.Fatalf("vector extension missing: %v", err)
	}

	assertTable(t, db, "documents")
	assertTable(t, db, "chunks")

	var formatted string
	err = db.QueryRowContext(ctx, `
		SELECT format_type(a.atttypid, a.atttypmod)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relname = 'chunks' AND a.attname = 'embedding'
	`).Scan(&formatted)
	if err != nil {
		t.Fatalf("embedding column: %v", err)
	}
	want := "vector(768)"
	if formatted != want {
		t.Fatalf("embedding type = %q, want %q (EmbeddingDim=%d)", formatted, want, EmbeddingDim)
	}

	if err := migrate(ctx, db); err != nil {
		t.Fatalf("migrate should be idempotent: %v", err)
	}

	var hasStatus, hasPage bool
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'documents' AND column_name = 'status'
		)`).Scan(&hasStatus); err != nil || !hasStatus {
		t.Fatalf("documents.status missing: %v", err)
	}
	if err := db.QueryRowContext(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_name = 'chunks' AND column_name = 'page_number'
		)`).Scan(&hasPage); err != nil || !hasPage {
		t.Fatalf("chunks.page_number missing: %v", err)
	}
}

func assertTable(t *testing.T, db *sql.DB, name string) {
	t.Helper()
	var rel string
	err := db.QueryRow(`SELECT relname FROM pg_class WHERE relname = $1 AND relkind = 'r'`, name).Scan(&rel)
	if err != nil {
		t.Fatalf("table %s missing: %v", name, err)
	}
}
