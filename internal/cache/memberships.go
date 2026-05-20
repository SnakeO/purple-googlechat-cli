// memberships.go handles CRUD for conversation membership records.
package cache

import "database/sql"

// UpsertMembership records that a user is a member of a conversation.
func (c *Cache) UpsertMembership(convID, userID string) error {
	_, err := c.db.Exec(`
		INSERT INTO memberships (conversation_id, user_id, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(conversation_id, user_id) DO UPDATE SET
			updated_at = excluded.updated_at`,
		convID, userID, now(),
	)
	return err
}

// ListMemberships returns all user IDs in a conversation.
func (c *Cache) ListMemberships(convID string) ([]string, error) {
	rows, err := c.db.Query(
		"SELECT user_id FROM memberships WHERE conversation_id = ?",
		convID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var userIDs []string
	for rows.Next() {
		var uid string
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		userIDs = append(userIDs, uid)
	}
	return userIDs, rows.Err()
}

// GetDMContactName returns the other person's name in a DM, excluding selfID.
// Joins memberships with users to resolve the name.
func (c *Cache) GetDMContactName(convID, selfID string) (string, error) {
	var name string
	err := c.db.QueryRow(`
		SELECT u.name FROM memberships m
		JOIN users u ON m.user_id = u.gaia_id
		WHERE m.conversation_id = ? AND m.user_id != ?
		LIMIT 1`,
		convID, selfID,
	).Scan(&name)

	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}
