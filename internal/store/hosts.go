package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Host is a server rootnet can connect to.
type Host struct {
	ID           int64      `json:"id" toml:"-"`
	Name         string     `json:"name" toml:"name"`
	User         string     `json:"user,omitempty" toml:"user,omitempty"`
	Hostname     string     `json:"hostname" toml:"hostname"`
	Port         int        `json:"port,omitempty" toml:"port,omitzero"`
	IdentityFile string     `json:"identity_file,omitempty" toml:"identity_file,omitempty"`
	JumpHost     string     `json:"jump_host,omitempty" toml:"jump_host,omitempty"`
	RemotePath   string     `json:"remote_path,omitempty" toml:"remote_path,omitempty"`
	ExtraArgs    string     `json:"extra_args,omitempty" toml:"extra_args,omitempty"`
	Notes        string     `json:"notes,omitempty" toml:"notes,omitempty"`
	Environment  string     `json:"environment,omitempty" toml:"environment,omitempty"`
	Tags         []string   `json:"tags,omitempty" toml:"tags,omitempty"`
	LastUsedAt   *time.Time `json:"last_used_at,omitempty" toml:"-"`
	UseCount     int        `json:"use_count" toml:"-"`
	CreatedAt    time.Time  `json:"created_at" toml:"-"`
	UpdatedAt    time.Time  `json:"updated_at" toml:"-"`
}

// Target returns the ssh destination, "user@hostname" or just "hostname".
func (h Host) Target() string {
	if h.User == "" {
		return h.Hostname
	}
	return h.User + "@" + h.Hostname
}

// Validate checks the fields required to save a host.
func (h Host) Validate() error {
	if strings.TrimSpace(h.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(h.Hostname) == "" {
		return errors.New("hostname is required")
	}
	if h.Port < 0 || h.Port > 65535 {
		return fmt.Errorf("invalid port %d", h.Port)
	}
	return nil
}

const hostColumns = `id, name, user, hostname, port, identity_file, jump_host, remote_path,
	extra_args, notes, environment, last_used_at, use_count, created_at, updated_at`

type scanner interface{ Scan(dest ...any) error }

func scanHost(r scanner) (Host, error) {
	var h Host
	var last sql.NullTime
	err := r.Scan(&h.ID, &h.Name, &h.User, &h.Hostname, &h.Port, &h.IdentityFile, &h.JumpHost,
		&h.RemotePath, &h.ExtraArgs, &h.Notes, &h.Environment, &last, &h.UseCount,
		&h.CreatedAt, &h.UpdatedAt)
	if last.Valid {
		t := last.Time
		h.LastUsedAt = &t
	}
	return h, err
}

// List returns all hosts ordered by name. If tag is non-empty only hosts
// carrying that tag are returned.
func (s *Store) List(ctx context.Context, tag string) ([]Host, error) {
	q := `SELECT ` + hostColumns + ` FROM hosts`
	var args []any
	if tag != "" {
		q += ` WHERE id IN (SELECT ht.host_id FROM host_tags ht JOIN tags t ON t.id = ht.tag_id WHERE t.name = ?)`
		args = append(args, normalizeTag(tag))
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var hosts []Host
	for rows.Next() {
		h, err := scanHost(rows)
		if err != nil {
			return nil, err
		}
		hosts = append(hosts, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	tags, err := s.allHostTags(ctx)
	if err != nil {
		return nil, err
	}
	for i := range hosts {
		hosts[i].Tags = tags[hosts[i].ID]
	}
	return hosts, nil
}

// Get returns the host with the given name (case-insensitive).
func (s *Store) Get(ctx context.Context, name string) (Host, error) {
	h, err := scanHost(s.db.QueryRowContext(ctx,
		`SELECT `+hostColumns+` FROM hosts WHERE name = ?`, name))
	if errors.Is(err, sql.ErrNoRows) {
		return Host{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return Host{}, err
	}
	h.Tags, err = s.hostTags(ctx, h.ID)
	return h, err
}

// Create inserts a new host and returns it with ID and timestamps set.
func (s *Store) Create(ctx context.Context, h Host) (Host, error) {
	if err := h.Validate(); err != nil {
		return Host{}, err
	}
	if h.Port == 0 {
		h.Port = 22
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Host{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO hosts
		(name, user, hostname, port, identity_file, jump_host, remote_path, extra_args, notes, environment)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		h.Name, h.User, h.Hostname, h.Port, h.IdentityFile, h.JumpHost, h.RemotePath,
		h.ExtraArgs, h.Notes, h.Environment)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Host{}, fmt.Errorf("a host named %q already exists", h.Name)
		}
		return Host{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Host{}, err
	}
	if err := setTags(ctx, tx, id, h.Tags); err != nil {
		return Host{}, err
	}
	if err := tx.Commit(); err != nil {
		return Host{}, err
	}
	return s.getByID(ctx, id)
}

// Update saves all editable fields of h (matched by ID).
func (s *Store) Update(ctx context.Context, h Host) (Host, error) {
	if err := h.Validate(); err != nil {
		return Host{}, err
	}
	if h.Port == 0 {
		h.Port = 22
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Host{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE hosts SET
		name = ?, user = ?, hostname = ?, port = ?, identity_file = ?, jump_host = ?,
		remote_path = ?, extra_args = ?, notes = ?, environment = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		h.Name, h.User, h.Hostname, h.Port, h.IdentityFile, h.JumpHost, h.RemotePath,
		h.ExtraArgs, h.Notes, h.Environment, h.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Host{}, fmt.Errorf("a host named %q already exists", h.Name)
		}
		return Host{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Host{}, fmt.Errorf("%w: id %d", ErrNotFound, h.ID)
	}
	if err := setTags(ctx, tx, h.ID, h.Tags); err != nil {
		return Host{}, err
	}
	if err := tx.Commit(); err != nil {
		return Host{}, err
	}
	return s.getByID(ctx, h.ID)
}

// Delete removes the host with the given ID.
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM hosts WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: id %d", ErrNotFound, id)
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM host_tags)`)
	return err
}

// MarkUsed bumps the usage statistics used for frecency ranking.
func (s *Store) MarkUsed(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE hosts SET use_count = use_count + 1, last_used_at = ? WHERE id = ?`,
		time.Now().UTC(), id)
	return err
}

// Tags returns all tag names in use.
func (s *Store) Tags(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT name FROM tags WHERE id IN (SELECT tag_id FROM host_tags) ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) getByID(ctx context.Context, id int64) (Host, error) {
	h, err := scanHost(s.db.QueryRowContext(ctx, `SELECT `+hostColumns+` FROM hosts WHERE id = ?`, id))
	if err != nil {
		return Host{}, err
	}
	h.Tags, err = s.hostTags(ctx, id)
	return h, err
}

func (s *Store) hostTags(ctx context.Context, id int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.name FROM tags t
		JOIN host_tags ht ON ht.tag_id = t.id WHERE ht.host_id = ? ORDER BY t.name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) allHostTags(ctx context.Context) (map[int64][]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT ht.host_id, t.name FROM host_tags ht
		JOIN tags t ON t.id = ht.tag_id ORDER BY t.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]string{}
	for rows.Next() {
		var id int64
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		out[id] = append(out[id], t)
	}
	return out, rows.Err()
}

func setTags(ctx context.Context, tx *sql.Tx, hostID int64, tags []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM host_tags WHERE host_id = ?`, hostID); err != nil {
		return err
	}
	for _, t := range NormalizeTags(tags) {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags (name) VALUES (?)`, t); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO host_tags (host_id, tag_id)
			SELECT ?, id FROM tags WHERE name = ?`, hostID, t); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM host_tags)`)
	return err
}

func normalizeTag(t string) string { return strings.ToLower(strings.TrimSpace(t)) }

// NormalizeTags lowercases, trims, de-duplicates and sorts tags. Entries may
// themselves contain comma-separated lists.
func NormalizeTags(tags []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, raw := range tags {
		for _, t := range strings.Split(raw, ",") {
			t = normalizeTag(t)
			if t == "" || seen[t] {
				continue
			}
			seen[t] = true
			out = append(out, t)
		}
	}
	sort.Strings(out)
	return out
}
