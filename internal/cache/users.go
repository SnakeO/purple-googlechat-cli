// users.go handles CRUD operations for cached users.
package cache

import "database/sql"

// CachedUser represents a user stored in the cache.
type CachedUser struct {
	GaiaID string
	Name   string
	Email  string
}

// UpsertUser inserts or updates a user in the cache.
func (c *Cache) UpsertUser(gaiaID, name, email string) error {
	_, err := c.db.Exec(`
		INSERT INTO users (gaia_id, name, email, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(gaia_id) DO UPDATE SET
			name = excluded.name,
			email = excluded.email,
			updated_at = excluded.updated_at`,
		gaiaID, name, email, now(),
	)
	return err
}

// GetUser retrieves a user by Gaia ID. Returns nil if not found.
func (c *Cache) GetUser(gaiaID string) (*CachedUser, error) {
	var u CachedUser
	err := c.db.QueryRow(
		"SELECT gaia_id, name, email FROM users WHERE gaia_id = ?",
		gaiaID,
	).Scan(&u.GaiaID, &u.Name, &u.Email)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// SearchUsers finds users whose name or email matches the query (case-insensitive).
func (c *Cache) SearchUsers(query string, limit int) ([]CachedUser, error) {
	pattern := "%" + query + "%"
	q := "SELECT gaia_id, name, email FROM users WHERE name LIKE ? OR email LIKE ? ORDER BY name"
	args := []interface{}{pattern, pattern}

	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := c.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []CachedUser
	for rows.Next() {
		var u CachedUser
		if err := rows.Scan(&u.GaiaID, &u.Name, &u.Email); err != nil {
			return nil, err
		}
		users = append(users, u)
	}
	return users, rows.Err()
}
