// conversations.go handles CRUD operations for cached conversations.
package cache

// CachedConversation represents a conversation stored in the cache.
type CachedConversation struct {
	ID      string
	Name    string
	IsDM    bool
	LastMsg string
	LastTime int64
}

// UpsertConversation inserts or updates a conversation in the cache.
func (c *Cache) UpsertConversation(id, name string, isDM bool, lastMsg string, lastTime int64) error {
	_, err := c.db.Exec(`
		INSERT INTO conversations (id, name, is_dm, last_msg, last_time, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			is_dm = excluded.is_dm,
			last_msg = excluded.last_msg,
			last_time = excluded.last_time,
			updated_at = excluded.updated_at
		WHERE name != excluded.name
			OR is_dm != excluded.is_dm
			OR last_msg != excluded.last_msg
			OR last_time != excluded.last_time`,
		id, name, isDM, lastMsg, lastTime, now(),
	)
	return err
}

// ResolveConversationName builds a display name for unnamed group chats
// by joining member names from the cache, excluding selfID.
func (c *Cache) ResolveConversationName(convID, selfID string) string {
	rows, err := c.db.Query(`
		SELECT u.name FROM memberships m
		JOIN users u ON m.user_id = u.gaia_id
		WHERE m.conversation_id = ? AND m.user_id != ? AND u.name != ''
		ORDER BY u.name
		LIMIT 5`, convID, selfID)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		rows.Scan(&name)
		names = append(names, name)
	}

	if len(names) == 0 {
		return ""
	}

	result := ""
	for i, n := range names {
		if i > 0 {
			result += ", "
		}
		result += n
	}
	return result
}

// ListConversations returns cached conversations ordered by last activity.
// Pass limit=0 for no limit.
func (c *Cache) ListConversations(limit int) ([]CachedConversation, error) {
	query := "SELECT id, name, is_dm, last_msg, last_time FROM conversations ORDER BY last_time DESC"
	if limit > 0 {
		query += " LIMIT ?"
	}

	var rows interface{ Next() bool; Scan(...interface{}) error; Err() error; Close() error }
	var err error
	if limit > 0 {
		rows, err = c.db.Query(query, limit)
	} else {
		rows, err = c.db.Query(query)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var convos []CachedConversation
	for rows.Next() {
		var conv CachedConversation
		if err := rows.Scan(&conv.ID, &conv.Name, &conv.IsDM, &conv.LastMsg, &conv.LastTime); err != nil {
			return nil, err
		}
		convos = append(convos, conv)
	}
	return convos, rows.Err()
}
