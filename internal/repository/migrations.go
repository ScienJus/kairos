package repository

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/sqlite/*.sql migrations/postgres/*.sql
var migrationFiles embed.FS

const migrationSeparator = "-- +kairos StatementBreak"

func (r *SQLRepository) migrate(ctx context.Context) error {
	migrationMu.Lock()
	defer migrationMu.Unlock()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if r.dialect == dialectPostgres {
		if _, err := tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock($1)", postgresMigrationLockID); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at_ns BIGINT NOT NULL
		)`); err != nil {
		return err
	}

	directory := path.Join("migrations", string(r.dialect))
	entries, err := fs.ReadDir(migrationFiles, directory)
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		version := strings.TrimSuffix(entry.Name(), ".sql")
		var exists bool
		query := "SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = ?)"
		if err := tx.QueryRowContext(ctx, rebind(r.dialect, query), version).Scan(&exists); err != nil {
			return err
		}
		if exists {
			continue
		}
		content, err := migrationFiles.ReadFile(path.Join(directory, entry.Name()))
		if err != nil {
			return err
		}
		for _, statement := range strings.Split(string(content), migrationSeparator) {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("migration %s: %w", version, err)
			}
		}
		insert := "INSERT INTO schema_migrations (version, applied_at_ns) VALUES (?, ?)"
		if _, err := tx.ExecContext(ctx, rebind(r.dialect, insert), version, time.Now().UTC().UnixNano()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func rebind(dialect dialect, query string) string {
	if dialect != dialectPostgres {
		return query
	}
	var builder strings.Builder
	parameter := 1
	for _, character := range query {
		if character != '?' {
			builder.WriteRune(character)
			continue
		}
		fmt.Fprintf(&builder, "$%d", parameter)
		parameter++
	}
	return builder.String()
}
