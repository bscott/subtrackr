package database

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := Initialize(filepath.Join(t.TempDir(), "test.db"))
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { sqlDB.Close() })
	_, err = sqlDB.Exec(`CREATE TABLE parents (id INTEGER PRIMARY KEY);
		CREATE TABLE children (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parents(id))`)
	require.NoError(t, err)
	return sqlDB
}

func TestEveryConnectionEnforcesForeignKeysAndUsesWAL(t *testing.T) {
	sqlDB := openTestDB(t)
	ctx := context.Background()

	// Hold several connections open at once so the pool has to open new ones.
	conns := make([]*sql.Conn, 5)
	for i := range conns {
		conn, err := sqlDB.Conn(ctx)
		require.NoError(t, err)
		defer conn.Close()
		conns[i] = conn
	}

	for i, conn := range conns {
		var enabled int
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&enabled))
		require.Equal(t, 1, enabled, "connection %d has foreign keys off", i)

		_, err := conn.ExecContext(ctx, "INSERT INTO children (parent_id) VALUES (999)")
		require.ErrorContains(t, err, "FOREIGN KEY constraint failed", "connection %d accepted an orphan row", i)

		var mode string
		require.NoError(t, conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode))
		require.Equal(t, "wal", mode, "connection %d", i)
	}
}

func TestConcurrentWritesWaitInsteadOfFailing(t *testing.T) {
	sqlDB := openTestDB(t)

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				// Read-then-write in a transaction: without a busy timeout and an
				// immediate write lock this fails with "database is locked".
				tx, err := sqlDB.Begin()
				if err != nil {
					errs <- err
					return
				}
				var n int
				if err := tx.QueryRow("SELECT COUNT(*) FROM parents").Scan(&n); err != nil {
					tx.Rollback()
					errs <- err
					return
				}
				if _, err := tx.Exec("INSERT INTO parents (id) VALUES (?)", w*1000+i); err != nil {
					tx.Rollback()
					errs <- fmt.Errorf("writer %d: %w", w, err)
					return
				}
				if err := tx.Commit(); err != nil {
					errs <- err
					return
				}
			}
		}(w)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	var n int
	require.NoError(t, sqlDB.QueryRow("SELECT COUNT(*) FROM parents").Scan(&n))
	require.Equal(t, 8*25, n)
}
