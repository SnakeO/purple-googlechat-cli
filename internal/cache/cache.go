// Package cache provides a local SQLite cache for Google Chat entities.
// Stores conversations, messages, users, and memberships with automatic
// upsert-on-conflict for seamless refresh from live API data.
package cache

import (
	"database/sql"
	"fmt"
	"time"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"
)

func init() {
	sqlite_vec.Auto()
}

// Cache wraps a SQLite database for local entity storage.
type Cache struct {
	db *sql.DB
}

// Open creates or opens a cache database and runs migrations.
func Open(dbPath string) (*Cache, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("cache: cannot open database: %w", err)
	}
	db.SetMaxOpenConns(1)

	c := &Cache{db: db}
	if err := c.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("cache: migration failed: %w", err)
	}

	return c, nil
}

// Close closes the database connection.
func (c *Cache) Close() error {
	return c.db.Close()
}

// DB returns the underlying database connection for direct queries.
func (c *Cache) DB() *sql.DB {
	return c.db
}

// migrate applies schema migrations using PRAGMA user_version.
func (c *Cache) migrate() error {
	var version int
	c.db.QueryRow("PRAGMA user_version").Scan(&version)

	if version < 1 {
		if err := c.migrateV1(); err != nil {
			return err
		}
	}

	return nil
}

// migrateV1 creates the initial schema.
func (c *Cache) migrateV1() error {
	schema := `
		CREATE TABLE IF NOT EXISTS conversations (
			id          TEXT PRIMARY KEY,
			name        TEXT NOT NULL DEFAULT '',
			is_dm       INTEGER NOT NULL DEFAULT 0,
			last_msg    TEXT NOT NULL DEFAULT '',
			last_time   INTEGER NOT NULL DEFAULT 0,
			updated_at  INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS users (
			gaia_id     TEXT PRIMARY KEY,
			name        TEXT NOT NULL DEFAULT '',
			email       TEXT NOT NULL DEFAULT '',
			updated_at  INTEGER NOT NULL DEFAULT 0
		);

		CREATE TABLE IF NOT EXISTS messages (
			conversation_id TEXT NOT NULL,
			message_id      TEXT NOT NULL,
			sender_id       TEXT NOT NULL DEFAULT '',
			text            TEXT NOT NULL DEFAULT '',
			created_at      INTEGER NOT NULL DEFAULT 0,
			is_deleted      INTEGER NOT NULL DEFAULT 0,
			updated_at      INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (conversation_id, message_id)
		);
		CREATE INDEX IF NOT EXISTS idx_messages_conv_time ON messages(conversation_id, created_at);
		CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender_id);

		CREATE TABLE IF NOT EXISTS memberships (
			conversation_id TEXT NOT NULL,
			user_id         TEXT NOT NULL,
			updated_at      INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (conversation_id, user_id)
		);

		CREATE TABLE IF NOT EXISTS cache_meta (
			key   TEXT PRIMARY KEY,
			value TEXT NOT NULL DEFAULT ''
		);

		CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			text,
			content=messages,
			content_rowid=rowid
		);

		CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages BEGIN
			INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
		END;
		CREATE TRIGGER IF NOT EXISTS messages_fts_update AFTER UPDATE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
			INSERT INTO messages_fts(rowid, text) VALUES (new.rowid, new.text);
		END;
		CREATE TRIGGER IF NOT EXISTS messages_fts_delete AFTER DELETE ON messages BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, text) VALUES ('delete', old.rowid, old.text);
		END;

		CREATE VIRTUAL TABLE IF NOT EXISTS vec_messages USING vec0(
			embedding float[768]
		);

		PRAGMA user_version = 1;
	`

	_, err := c.db.Exec(schema)
	return err
}

// now returns the current time in microseconds.
func now() int64 {
	return time.Now().UnixMicro()
}

// SetMeta stores a key-value pair in the cache_meta table.
func (c *Cache) SetMeta(key, value string) error {
	_, err := c.db.Exec(
		"INSERT OR REPLACE INTO cache_meta (key, value) VALUES (?, ?)",
		key, value,
	)
	return err
}

// GetMeta retrieves a value from the cache_meta table. Returns "" if not found.
func (c *Cache) GetMeta(key string) (string, error) {
	var value string
	err := c.db.QueryRow("SELECT value FROM cache_meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}
