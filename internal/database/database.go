package database

import (
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// connectionParams are applied by mattn/go-sqlite3 to every connection the
// pool opens, so they hold no matter which pooled connection runs a query:
//   - _foreign_keys: FK enforcement is per connection in SQLite.
//   - _busy_timeout: wait for a competing writer instead of failing with
//     "database is locked".
//   - _journal_mode=WAL: readers no longer block the writer (persisted in the
//     database file; creates -wal/-shm files next to it).
//   - _txlock=immediate: transactions take the write lock at BEGIN, so a
//     read-then-write transaction waits on the busy timeout instead of
//     failing when another writer got in first.
const connectionParams = "_foreign_keys=on&_busy_timeout=5000&_journal_mode=WAL&_txlock=immediate"

func Initialize(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath+"?"+connectionParams), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, err
	}

	return db, nil
}
