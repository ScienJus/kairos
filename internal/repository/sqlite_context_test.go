package repository

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/ScienJus/kairos/internal/application"
)

func TestSQLiteWriteTransactionHonorsContextWhileWaiting(t *testing.T) {
	for _, mode := range []string{"memory", "wal"} {
		t.Run(mode, func(t *testing.T) {
			path := ":memory:"
			if mode == "wal" {
				path = filepath.Join(t.TempDir(), "context.db")
			}
			repo, err := OpenSQLite(context.Background(), path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = repo.Close() })

			// Hold the write lock on one pooled connection. The second
			// transaction must honor its context both for shared-cache memory
			// databases and for a file database's five-second busy timeout.
			holder, err := repo.db.BeginTx(context.Background(), &sql.TxOptions{Isolation: sql.LevelSerializable})
			if err != nil {
				t.Fatal(err)
			}
			defer holder.Rollback()

			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			called := false
			start := time.Now()
			err = repo.Update(ctx, func(application.WriteStore) error {
				called = true
				return nil
			})
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("blocked write error = %v, want context deadline exceeded", err)
			}
			if called {
				t.Fatal("blocked transaction executed its operation")
			}
			if elapsed := time.Since(start); elapsed > 2*time.Second {
				t.Fatalf("context cancellation took %s, want less than the five-second busy timeout", elapsed)
			}
			if err := holder.Rollback(); err != nil {
				t.Fatal(err)
			}

			// Cancellation must not leave a pooled connection or lock unusable.
			recoveryCtx, recoveryCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer recoveryCancel()
			if err := repo.Update(recoveryCtx, func(application.WriteStore) error { return nil }); err != nil {
				t.Fatalf("write after cancellation: %v", err)
			}
		})
	}
}
