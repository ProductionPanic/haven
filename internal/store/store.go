// Package store persists hosts in a SQLite database.
package store

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// ErrNotFound is returned when a host does not exist.
var ErrNotFound = errors.New("host not found")

// Store is a handle to the haven database.
type Store struct {
	db *sql.DB
}

// EnvPath returns the database path from $HAVEN_DB, or from $ROOTNET_DB
// (the tool's former name), or "".
func EnvPath() string {
	if p := os.Getenv("HAVEN_DB"); p != "" {
		return p
	}
	return os.Getenv("ROOTNET_DB")
}

func configDir() (string, error) {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config"), nil
}

// DefaultPath returns the database location: $HAVEN_DB (or $ROOTNET_DB),
// else $XDG_CONFIG_HOME/haven/haven.db, else ~/.config/haven/haven.db.
func DefaultPath() (string, error) {
	if p := EnvPath(); p != "" {
		return p, nil
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "haven", "haven.db"), nil
}

// MoveLegacy moves the database the tool used under its former name
// (<config>/rootnet/rootnet.db, with its WAL files) to path, if path does
// not exist yet. It returns the old location when something was moved.
func MoveLegacy(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return "", nil
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	oldDir := filepath.Join(dir, "rootnet")
	old := filepath.Join(oldDir, "rootnet.db")
	if _, err := os.Stat(old); err != nil {
		return "", nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		err := os.Rename(old+suffix, path+suffix)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("moving %s to %s: %w", old+suffix, path+suffix, err)
		}
	}
	_ = os.Remove(oldDir) // only succeeds when empty
	return old, nil
}

// Open opens (and creates if needed) the database at path and applies
// any pending migrations.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection keeps :memory: databases coherent and avoids
	// writer contention; haven never needs parallel queries.
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	files, err := fs.Glob(migrationFS, "migrations/*.sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	for _, f := range files {
		n, err := strconv.Atoi(strings.SplitN(filepath.Base(f), "_", 2)[0])
		if err != nil {
			return fmt.Errorf("bad migration name %q", f)
		}
		if n <= version {
			continue
		}
		body, err := migrationFS.ReadFile(f)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("%s: %w", f, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", n)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
