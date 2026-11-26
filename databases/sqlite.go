package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// OpenSequentialSQLite opens a modernc.org/sqlite database tuned for fast
// sequential reads and writes:
//   - Single pooled connection (MaxOpenConns=1) eliminates SQLITE_BUSY write
//     contention across goroutines.
//   - WAL journal mode + synchronous=NORMAL turns writes into fast sequential
//     appends instead of full rollback-journal fsyncs.
//   - 64 MB RAM page cache, in-memory temp store, and 256 MB mmap speed up
//     sequential scans and chat history queries.
func OpenSequentialSQLite(dbPath string) (*sql.DB, error) {
	if dir := filepath.Dir(dbPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create sqlite dir: %w", err)
		}
	}

	dsn := fmt.Sprintf(
		"file:%s?_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(10000)&_pragma=cache_size(-64000)&_pragma=temp_store(MEMORY)&_pragma=mmap_size(268435456)&_pragma=wal_autocheckpoint(1000)",
		dbPath,
	)

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}

	// Strictly sequential single connection: no lock contention, predictable I/O.
	conn.SetMaxOpenConns(1)
	conn.SetMaxIdleConns(1)
	conn.SetConnMaxLifetime(0)

	// Explicitly verify and enforce pragmas on the active connection.
	pragmas := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=10000;",
		"PRAGMA cache_size=-64000;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA mmap_size=268435456;",
		"PRAGMA wal_autocheckpoint=1000;",
	}
	for _, p := range pragmas {
		if _, err := conn.Exec(p); err != nil {
			conn.Close()
			return nil, fmt.Errorf("apply %s: %w", p, err)
		}
	}

	return conn, nil
}

// CheckpointAndClose flushes the WAL into the main database file and closes it.
func CheckpointAndClose(conn *sql.DB) error {
	if conn == nil {
		return nil
	}
	_, _ = conn.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return conn.Close()
}

// SQLiteStatus reads active PRAGMA settings for diagnostics/status endpoints.
func SQLiteStatus(conn *sql.DB) map[string]string {
	status := map[string]string{}
	if conn == nil {
		return status
	}
	for _, name := range []string{"journal_mode", "synchronous", "foreign_keys", "busy_timeout", "cache_size"} {
		var val string
		if err := conn.QueryRow("PRAGMA " + name + ";").Scan(&val); err == nil {
			status[name] = strings.ToLower(val)
		}
	}
	return status
}
