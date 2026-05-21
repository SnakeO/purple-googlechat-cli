// search.go provides semantic and keyword search across cached messages.
// Semantic search uses sqlite-vec for vector similarity.
// Keyword search uses FTS5 for exact text matching.
package cache

import (
	"database/sql"
	"fmt"

	sqlite_vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// SearchResult represents a single search hit with context.
type SearchResult struct {
	ConversationID string
	MessageID      string
	SenderID       string
	SenderName     string
	Text           string
	CreatedAt      int64
	Distance       float64
}

// UpsertMessageEmbedding stores an embedding vector for a message.
// The rowid must match the message's rowid in the messages table.
func (c *Cache) UpsertMessageEmbedding(msgRowID int64, embedding []float32) error {
	serialized, err := sqlite_vec.SerializeFloat32(embedding)
	if err != nil {
		return fmt.Errorf("cache: cannot serialize embedding: %w", err)
	}

	_, err = c.db.Exec(
		"INSERT OR REPLACE INTO vec_messages(rowid, embedding) VALUES (?, ?)",
		msgRowID, serialized,
	)
	return err
}

// SemanticSearch finds messages similar to the query embedding.
// Joins with users for sender names. Optionally filters by time.
func (c *Cache) SemanticSearch(queryEmbedding []float32, limit int, sinceUsec int64) ([]SearchResult, error) {
	serialized, err := sqlite_vec.SerializeFloat32(queryEmbedding)
	if err != nil {
		return nil, fmt.Errorf("cache: cannot serialize query: %w", err)
	}

	if limit <= 0 {
		limit = 20
	}

	// Get matching rowids from vector search, then join with messages and users
	query := `
		SELECT m.conversation_id, m.message_id, m.sender_id,
			COALESCE(u.name, ''), m.text, m.created_at, v.distance
		FROM vec_messages v
		JOIN messages m ON m.rowid = v.rowid
		LEFT JOIN users u ON m.sender_id = u.gaia_id
		WHERE v.embedding MATCH ?
			AND k = ?
			AND m.is_deleted = 0`
	args := []interface{}{serialized, limit}

	if sinceUsec > 0 {
		query += " AND m.created_at >= ?"
		args = append(args, sinceUsec)
	}

	query += " ORDER BY v.distance"

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("cache: semantic search failed: %w", err)
	}
	defer rows.Close()

	return scanSearchResults(rows)
}

// KeywordSearch finds messages matching the query using FTS5.
// Falls back to LIKE search if the query contains FTS5 special characters.
func (c *Cache) KeywordSearch(query string, limit int, sinceUsec int64) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 20
	}

	if containsFTS5Special(query) {
		return c.likeSearch(query, limit, sinceUsec)
	}

	q := `
		SELECT m.conversation_id, m.message_id, m.sender_id,
			COALESCE(u.name, ''), m.text, m.created_at, 0.0
		FROM messages_fts fts
		JOIN messages m ON m.rowid = fts.rowid
		LEFT JOIN users u ON m.sender_id = u.gaia_id
		WHERE messages_fts MATCH ?
			AND m.is_deleted = 0`
	args := []interface{}{query}

	if sinceUsec > 0 {
		q += " AND m.created_at >= ?"
		args = append(args, sinceUsec)
	}

	q += " ORDER BY rank LIMIT ?"
	args = append(args, limit)

	rows, err := c.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("cache: keyword search failed: %w", err)
	}
	defer rows.Close()

	return scanSearchResults(rows)
}

// containsFTS5Special returns true if the query has characters that break FTS5.
func containsFTS5Special(query string) bool {
	for _, c := range query {
		switch c {
		case '@', '"', '*', '(', ')', '{', '}', '^', '~', ':', '-', '+', '!', '#':
			return true
		}
	}
	return false
}

// likeSearch falls back to SQL LIKE when the query has FTS5-incompatible characters.
func (c *Cache) likeSearch(query string, limit int, sinceUsec int64) ([]SearchResult, error) {
	pattern := "%" + query + "%"
	q := `
		SELECT m.conversation_id, m.message_id, m.sender_id,
			COALESCE(u.name, ''), m.text, m.created_at, 0.0
		FROM messages m
		LEFT JOIN users u ON m.sender_id = u.gaia_id
		WHERE m.text LIKE ?
			AND m.is_deleted = 0`
	args := []interface{}{pattern}

	if sinceUsec > 0 {
		q += " AND m.created_at >= ?"
		args = append(args, sinceUsec)
	}

	q += " ORDER BY m.created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := c.db.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("cache: like search failed: %w", err)
	}
	defer rows.Close()

	return scanSearchResults(rows)
}

// scanSearchResults reads rows into SearchResult structs.
func scanSearchResults(rows *sql.Rows) ([]SearchResult, error) {
	var results []SearchResult
	for rows.Next() {
		var r SearchResult
		if err := rows.Scan(&r.ConversationID, &r.MessageID, &r.SenderID,
			&r.SenderName, &r.Text, &r.CreatedAt, &r.Distance); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// SearchMentions finds messages that @mention the given user.
// Looks for messages containing "@" where the sender is not the user themselves.
// Also matches messages from other users that contain the user's cached display name.
func (c *Cache) SearchMentions(selfGaiaID string, limit int, sinceUsec int64) ([]SearchResult, error) {
	if limit <= 0 {
		limit = 50
	}

	// Get self user's name to search for @mentions
	selfName := ""
	u, _ := c.GetUser(selfGaiaID)
	if u != nil && u.Name != "" {
		selfName = u.Name
	}

	query := `
		SELECT m.conversation_id, m.message_id, m.sender_id,
			COALESCE(u.name, ''), m.text, m.created_at, 0.0
		FROM messages m
		LEFT JOIN users u ON m.sender_id = u.gaia_id
		WHERE m.is_deleted = 0
			AND m.sender_id != ?
			AND m.text LIKE '%@%'`
	args := []interface{}{selfGaiaID}

	if sinceUsec > 0 {
		query += " AND m.created_at >= ?"
		args = append(args, sinceUsec)
	}

	query += " ORDER BY m.created_at DESC LIMIT ?"
	args = append(args, limit)

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("cache: mentions search failed: %w", err)
	}
	defer rows.Close()

	results, err := scanSearchResults(rows)
	if err != nil {
		return nil, err
	}

	// If we have the user's name, also filter for messages containing it
	if selfName != "" && len(results) == 0 {
		query2 := `
			SELECT m.conversation_id, m.message_id, m.sender_id,
				COALESCE(u.name, ''), m.text, m.created_at, 0.0
			FROM messages m
			LEFT JOIN users u ON m.sender_id = u.gaia_id
			WHERE m.is_deleted = 0
				AND m.sender_id != ?
				AND m.text LIKE ?
			ORDER BY m.created_at DESC LIMIT ?`
		rows2, err := c.db.Query(query2, selfGaiaID, "%@"+selfName+"%", limit)
		if err != nil {
			return results, nil
		}
		defer rows2.Close()
		return scanSearchResults(rows2)
	}

	return results, nil
}

// Stats returns row counts for each cached entity type.
type CacheStats struct {
	Conversations int
	Messages      int
	Users         int
	Memberships   int
	Embeddings    int
}

// GetStats returns cache statistics.
func (c *Cache) GetStats() (*CacheStats, error) {
	stats := &CacheStats{}

	c.db.QueryRow("SELECT count(*) FROM conversations").Scan(&stats.Conversations)
	c.db.QueryRow("SELECT count(*) FROM messages").Scan(&stats.Messages)
	c.db.QueryRow("SELECT count(*) FROM users").Scan(&stats.Users)
	c.db.QueryRow("SELECT count(*) FROM memberships").Scan(&stats.Memberships)
	c.db.QueryRow("SELECT count(*) FROM vec_messages").Scan(&stats.Embeddings)

	return stats, nil
}

// ClearAll drops and recreates all tables.
func (c *Cache) ClearAll() error {
	tables := []string{"messages", "conversations", "users", "memberships", "cache_meta", "vec_messages", "messages_fts"}
	for _, table := range tables {
		c.db.Exec("DROP TABLE IF EXISTS " + table)
	}
	c.db.Exec("PRAGMA user_version = 0")
	return c.migrate()
}
