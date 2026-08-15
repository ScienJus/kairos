// Package repository provides SQL-backed implementations of the application ports.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/ScienJus/kairos/internal/application"
	"github.com/ScienJus/kairos/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	modernsqlite "modernc.org/sqlite"
)

type dialect string

const (
	dialectSQLite   dialect = "sqlite"
	dialectPostgres dialect = "postgres"

	postgresMigrationLockID int64 = 0x4b4149524f53
)

// SQLRepository persists Kairos aggregates through database/sql.
type SQLRepository struct {
	db      *sql.DB
	dialect dialect
}

var _ application.Repository = (*SQLRepository)(nil)

var sqliteMemorySequence atomic.Uint64
var migrationMu sync.Mutex

// OpenSQLite opens and migrates one SQLite database file.
func OpenSQLite(ctx context.Context, path string) (*SQLRepository, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("sqlite path is required")
	}
	dsn := sqliteDSN(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	repository := &SQLRepository{db: db, dialect: dialectSQLite}
	if err := repository.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return repository, nil
}

// OpenPostgres opens and migrates one PostgreSQL database.
func OpenPostgres(ctx context.Context, dsn string) (*SQLRepository, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, fmt.Errorf("postgres dsn is required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	repository := &SQLRepository{db: db, dialect: dialectPostgres}
	if err := repository.initialize(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return repository, nil
}

func sqliteDSN(path string) string {
	if path == ":memory:" {
		name := fmt.Sprintf("kairos-memory-%d", sqliteMemorySequence.Add(1))
		return "file:" + name + "?mode=memory&cache=shared&_txlock=immediate&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
	}
	uri := url.URL{Scheme: "file", Path: path}
	query := uri.Query()
	query.Add("_pragma", "foreign_keys(1)")
	query.Add("_pragma", "busy_timeout(5000)")
	query.Add("_pragma", "journal_mode(WAL)")
	query.Add("_txlock", "immediate")
	uri.RawQuery = query.Encode()
	return uri.String()
}

func (r *SQLRepository) initialize(ctx context.Context) error {
	if err := r.db.PingContext(ctx); err != nil {
		return fmt.Errorf("ping %s: %w", r.dialect, err)
	}
	if err := r.migrate(ctx); err != nil {
		return fmt.Errorf("migrate %s: %w", r.dialect, err)
	}
	return nil
}

// Close closes the underlying database connection pool.
func (r *SQLRepository) Close() error {
	return r.db.Close()
}

// View runs a consistent read operation.
func (r *SQLRepository) View(ctx context.Context, operation func(application.ReadStore) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return normalizeError(err)
	}
	store := &sqlStore{ctx: ctx, tx: tx, dialect: r.dialect}
	if err := operation(store); err != nil {
		_ = tx.Rollback()
		return err
	}
	return normalizeError(tx.Commit())
}

// Update atomically commits one application operation.
func (r *SQLRepository) Update(ctx context.Context, operation func(application.WriteStore) error) error {
	return r.withWriteTransaction(ctx, func(store *sqlStore) error {
		return operation(store)
	})
}

func (r *SQLRepository) withWriteTransaction(ctx context.Context, operation func(*sqlStore) error) error {
	isolation := sql.LevelReadCommitted
	if r.dialect == dialectSQLite {
		isolation = sql.LevelSerializable
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: isolation})
	if err != nil {
		return normalizeError(err)
	}
	store := &sqlStore{ctx: ctx, tx: tx, dialect: r.dialect, writable: true}
	if err := operation(store); err != nil {
		_ = tx.Rollback()
		return err
	}
	return normalizeError(tx.Commit())
}

// CreateWorkflowDefinition stores one immutable Workflow Definition version.
func (r *SQLRepository) CreateWorkflowDefinition(ctx context.Context, definition domain.WorkflowDefinition) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	return r.createDefinition(ctx, definition.ID, definition.Version, domain.CoordinationModeWorkflow, definition)
}

// CreateBlackboardDefinition stores one immutable Blackboard Definition version.
func (r *SQLRepository) CreateBlackboardDefinition(ctx context.Context, definition domain.BlackboardDefinition) error {
	if err := definition.Validate(); err != nil {
		return err
	}
	return r.createDefinition(ctx, definition.ID, definition.Version, domain.CoordinationModeBlackboard, definition)
}

func (r *SQLRepository) createDefinition(
	ctx context.Context,
	id domain.DefinitionID,
	version int64,
	mode domain.CoordinationMode,
	definition any,
) error {
	payload, err := encodeJSON(definition)
	if err != nil {
		return err
	}
	return r.withWriteTransaction(ctx, func(store *sqlStore) error {
		_, err := store.exec(
			"INSERT INTO definitions (id, version, mode, payload) VALUES (?, ?, ?, ?)",
			id, version, mode, payload,
		)
		return err
	})
}

func normalizeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return application.ErrNotFound
	}
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) {
		switch postgresError.Code {
		case "23503", "23505", "40001", "40P01", "55P03":
			return fmt.Errorf("%w: %s", application.ErrConflict, postgresError.Message)
		}
	}
	var sqliteError *modernsqlite.Error
	if errors.As(err, &sqliteError) {
		switch sqliteError.Code() & 0xff {
		case 5, 6, 19:
			return fmt.Errorf("%w: %s", application.ErrConflict, sqliteError.Error())
		}
	}
	return err
}
