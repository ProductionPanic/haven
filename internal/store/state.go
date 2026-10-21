package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
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

// SaveCheck records the outcome of an ssh check of a host.
func (s *Store) SaveCheck(ctx context.Context, hostID int64, c Check) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO host_state (host_id, last_check_at, last_check_ok, last_check_error)
		VALUES (?, ?, ?, ?)
		ON CONFLICT (host_id) DO UPDATE SET last_check_at = excluded.last_check_at,
			last_check_ok = excluded.last_check_ok, last_check_error = excluded.last_check_error`,
		hostID, c.At.UTC(), c.OK, c.Error)
	return err
}

// Stale reports whether h has not been checked within maxAge.
func (h Host) Stale(now time.Time, maxAge time.Duration) bool {
	return h.Check == nil || now.Sub(h.Check.At) >= maxAge
}
