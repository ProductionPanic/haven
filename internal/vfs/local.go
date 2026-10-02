package vfs

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// LocalFS is the local file system.
type LocalFS struct{}

func (LocalFS) Name() string { return "Local" }
func (LocalFS) Remote() bool { return false }

func (l LocalFS) ReadDir(path string) ([]Entry, error) {
	des, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		fi, err := de.Info()
		if err != nil {
			continue // vanished between readdir and lstat
		}
		out = append(out, l.resolve(filepath.Join(path, de.Name()), fi))
	}
	return out, nil
}

func (LocalFS) resolve(path string, fi fs.FileInfo) Entry {
	e := entryFromInfo(fi)
	if fi.Mode()&fs.ModeSymlink == 0 {
		return e
	}
	e.Link, _ = os.Readlink(path)
	if target, err := os.Stat(path); err == nil {
		e.IsDir, e.Size = target.IsDir(), target.Size()
	} else {
		e.Broken = true
	}
	return e
}

func (l LocalFS) Stat(path string) (Entry, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return Entry{}, err
	}
	return l.resolve(path, fi), nil
}

func (LocalFS) Open(path string) (io.ReadCloser, error) { return os.Open(path) }

func (LocalFS) Create(path string) (io.WriteCloser, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
}

func (LocalFS) Mkdir(path string) error    { return os.Mkdir(path, 0o755) }
func (LocalFS) MkdirAll(path string) error { return os.MkdirAll(path, 0o755) }
func (LocalFS) Remove(path string) error   { return os.RemoveAll(path) }
func (LocalFS) Rename(from, to string) error {
	return os.Rename(from, to)
}
func (LocalFS) Chtimes(path string, mtime time.Time) error {
	return os.Chtimes(path, mtime, mtime)
}
func (LocalFS) Chmod(path string, mode fs.FileMode) error { return os.Chmod(path, mode) }

func (LocalFS) Home() (string, error)      { return os.UserHomeDir() }
func (LocalFS) Join(elem ...string) string { return filepath.Join(elem...) }
func (LocalFS) Dir(path string) string     { return filepath.Dir(path) }
func (LocalFS) Base(path string) string    { return filepath.Base(path) }
