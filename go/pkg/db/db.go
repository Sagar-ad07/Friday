package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

var (
	db   *sql.DB
	once sync.Once
)

// Init opens the SQLite database at the given path and runs migrations.
// It uses WAL mode for concurrent read/write performance.
func Init(dbPath string) error {
	var initErr error
	once.Do(func() {
		os.MkdirAll(filepath.Dir(dbPath), 0755)

		conn, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)")
		if err != nil {
			initErr = fmt.Errorf("db open: %w", err)
			return
		}
		conn.SetMaxOpenConns(10)
		conn.SetMaxIdleConns(5)

		if err := conn.Ping(); err != nil {
			initErr = fmt.Errorf("db ping: %w", err)
			return
		}

		db = conn
		initErr = migrate(db)
	})
	return initErr
}

// Get returns the global database connection.
func Get() *sql.DB {
	if db == nil {
		log.Fatal("database not initialized: call db.Init() first")
	}
	return db
}


// SafeGet returns the global DB connection, or nil if not initialized.
func SafeGet() *sql.DB {
	return db
}

// Close closes the database connection.
func Close() error {
	if db != nil {
		return db.Close()
	}
	return nil
}

func migrate(d *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS memory_facts (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fact TEXT NOT NULL,
			type TEXT NOT NULL DEFAULT 'SEMANTIC',
			verified INTEGER NOT NULL DEFAULT 0,
			tags TEXT DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_type ON memory_facts(type)`,
		`CREATE INDEX IF NOT EXISTS idx_memory_verified ON memory_facts(verified)`,
		`CREATE VIRTUAL TABLE IF NOT EXISTS memory_fts USING fts5(fact, content='memory_facts', content_rowid=id)`,

		`CREATE TABLE IF NOT EXISTS decisions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			input TEXT NOT NULL,
			tool_calls TEXT DEFAULT '',
			result TEXT DEFAULT '',
			outcome TEXT DEFAULT '',
			confidence REAL DEFAULT 0,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_decisions_created ON decisions(created_at DESC)`,

		`CREATE TABLE IF NOT EXISTS accounts (
			name TEXT PRIMARY KEY,
			login INTEGER NOT NULL DEFAULT 0,
			server TEXT DEFAULT '',
			password TEXT DEFAULT '',
			balance REAL DEFAULT 0,
			currency TEXT DEFAULT 'USD',
			type TEXT DEFAULT 'private',
			active INTEGER DEFAULT 0
		)`,

		`CREATE TABLE IF NOT EXISTS kv_store (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, m := range migrations {
		if _, err := d.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w\nSQL: %s", err, m)
		}
	}

	// Seed FTS trigger for memory_facts
	triggers := []string{
		`CREATE TRIGGER IF NOT EXISTS memory_fts_ai AFTER INSERT ON memory_facts BEGIN
			INSERT INTO memory_fts(rowid, fact) VALUES (new.id, new.fact);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memory_fts_ad AFTER DELETE ON memory_facts BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, fact) VALUES('delete', old.id, old.fact);
		END`,
		`CREATE TRIGGER IF NOT EXISTS memory_fts_au AFTER UPDATE ON memory_facts BEGIN
			INSERT INTO memory_fts(memory_fts, rowid, fact) VALUES('delete', old.id, old.fact);
			INSERT INTO memory_fts(rowid, fact) VALUES (new.id, new.fact);
		END`,
	}
	for _, t := range triggers {
		if _, err := d.Exec(t); err != nil {
			return fmt.Errorf("trigger failed: %w", err)
		}
	}

	log.Println("[DB] SQLite migrations complete")
	return nil
}

// KVGet retrieves a value from the key-value store.
func KVGet(key string) (string, error) {
	var val string
	err := Get().QueryRow("SELECT value FROM kv_store WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// KVSet stores a value in the key-value store.
func KVSet(key, value string) error {
	_, err := Get().Exec("INSERT INTO kv_store (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP) ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP", key, value)
	return err
}

