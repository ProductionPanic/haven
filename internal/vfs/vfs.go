// Package vfs gives the local disk and a remote host the same file system
// interface, so the file manager and transfer engine treat them alike.
package vfs

import (
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"
)

// Entry is a directory entry. For symlinks, IsDir/Size describe the target
// and Link holds the link destination.
type Entry struct {
	Name    string
	Size    int64
	Mode    fs.FileMode
	ModTime time.Time
	IsDir   bool
	Link    string // symlink target, "" if not a link
	Broken  bool   // symlink whose target can't be resolved
}

// Hidden reports whether the entry is a dotfile.
func (e Entry) Hidden() bool { return strings.HasPrefix(e.Name, ".") }

// FS is implemented by LocalFS and RemoteFS. Paths are absolute and use
// the file system's own separator.
type FS interface {
	// Name labels the file system in the UI ("Local" or a host name).
	Name() string
	// Remote reports whether this is a remote file system.
	Remote() bool

	ReadDir(path string) ([]Entry, error)
	Stat(path string) (Entry, error)
	Open(path string) (io.ReadCloser, error)
	// Create creates or truncates a file for writing.
	Create(path string) (io.WriteCloser, error)
	Mkdir(path string) error
	MkdirAll(path string) error
	// Remove deletes a file, or a directory recursively.
	Remove(path string) error
	// Rename moves from to to, replacing to if it exists.
	Rename(from, to string) error
	Chtimes(path string, mtime time.Time) error
	Chmod(path string, mode fs.FileMode) error

	Home() (string, error)
	Join(elem ...string) string
	Dir(path string) string
	Base(path string) string
}

// SortBy is a directory listing order.
type SortBy int

const (
	SortName SortBy = iota
	SortSize
	SortTime
)

func (s SortBy) String() string {
	return [...]string{"name", "size", "modified"}[s]
}

// Sort orders entries with directories first, then by the given key.
// Size and time sort largest/newest first.
func Sort(entries []Entry, by SortBy) {
	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		switch by {
		case SortSize:
			if a.Size != b.Size {
				return a.Size > b.Size
			}
		case SortTime:
			if !a.ModTime.Equal(b.ModTime) {
				return a.ModTime.After(b.ModTime)
			}
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
}

func entryFromInfo(fi fs.FileInfo) Entry {
	return Entry{
		Name:    fi.Name(),
		Size:    fi.Size(),
		Mode:    fi.Mode(),
		ModTime: fi.ModTime(),
		IsDir:   fi.IsDir(),
	}
}
