// messages.go handles CRUD operations for cached messages.
package cache

// CachedMessage represents a message stored in the cache.
type CachedMessage struct {
	ConversationID string
	MessageID      string
	SenderID       string
	Text           string
	CreatedAt      int64
	IsDeleted      bool
}

// UpsertMessage inserts or updates a message in the cache.
// Skips the update if the existing row already has identical data.
func (c *Cache) UpsertMessage(convID, msgID, senderID, text string, createdAt int64, isDeleted bool) error {
	_, err := c.db.Exec(`
		INSERT INTO messages (conversation_id, message_id, sender_id, text, created_at, is_deleted, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id, message_id) DO UPDATE SET
			sender_id = excluded.sender_id,
			text = excluded.text,
			created_at = excluded.created_at,
			is_deleted = excluded.is_deleted,
			updated_at = excluded.updated_at
		WHERE sender_id != excluded.sender_id
			OR text != excluded.text
			OR created_at != excluded.created_at
			OR is_deleted != excluded.is_deleted`,
		convID, msgID, senderID, text, createdAt, isDeleted, now(),
	)
	return err
}

// ListMessages returns cached messages for a conversation.
// limit=0 means no limit. sinceUsec=0 means no time filter.
func (c *Cache) ListMessages(convID string, limit int, sinceUsec int64) ([]CachedMessage, error) {
	query := "SELECT conversation_id, message_id, sender_id, text, created_at, is_deleted FROM messages WHERE conversation_id = ?"
	args := []interface{}{convID}

	if sinceUsec > 0 {
		query += " AND created_at >= ?"
		args = append(args, sinceUsec)
	}

	query += " ORDER BY created_at ASC"

	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := c.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var msgs []CachedMessage
	for rows.Next() {
		var m CachedMessage
		if err := rows.Scan(&m.ConversationID, &m.MessageID, &m.SenderID, &m.Text, &m.CreatedAt, &m.IsDeleted); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

// MarkDeleted sets is_deleted=1 for a specific message.
func (c *Cache) MarkDeleted(convID, msgID string) error {
	_, err := c.db.Exec(
		"UPDATE messages SET is_deleted = 1, updated_at = ? WHERE conversation_id = ? AND message_id = ?",
		now(), convID, msgID,
	)
	return err
}

// MessageRowID returns the rowid for a message. Used for linking to vec_messages.
func (c *Cache) MessageRowID(convID, msgID string) (int64, error) {
	var rowid int64
	err := c.db.QueryRow(
		"SELECT rowid FROM messages WHERE conversation_id = ? AND message_id = ?",
		convID, msgID,
	).Scan(&rowid)
	return rowid, err
}
