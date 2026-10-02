package store

import (
	"context"
	"database/sql"
	"errors"
)

// Dirs returns the last local and remote directories used in the file
// manager for a host ("" when unknown).
func (s *Store) Dirs(ctx context.Context, hostID int64) (local, remote string, err error) {
	err = s.db.QueryRowContext(ctx,
		`SELECT last_local_dir, last_remote_dir FROM host_state WHERE host_id = ?`, hostID).
		Scan(&local, &remote)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	}
	return local, remote, err
}

// SaveDirs remembers the file manager directories for a host.
func (s *Store) SaveDirs(ctx context.Context, hostID int64, local, remote string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO host_state (host_id, last_local_dir, last_remote_dir)
		VALUES (?, ?, ?)
		ON CONFLICT (host_id) DO UPDATE SET last_local_dir = excluded.last_local_dir,
			last_remote_dir = excluded.last_remote_dir`, hostID, local, remote)
	return err
}
