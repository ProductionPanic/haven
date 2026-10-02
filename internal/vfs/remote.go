package vfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

// RemoteFS is a remote host's file system over SFTP. Paths are POSIX.
type RemoteFS struct {
	name   string
	client *sftp.Client
	closer func() error
}

// NewRemoteFS wraps an SFTP client. closer (optional) is called by Close
// after the client is closed, e.g. to reap the ssh process.
func NewRemoteFS(name string, c *sftp.Client, closer func() error) *RemoteFS {
	return &RemoteFS{name: name, client: c, closer: closer}
}

// Close ends the SFTP session.
func (r *RemoteFS) Close() error {
	err := r.client.Close()
	if r.closer != nil {
		if cerr := r.closer(); err == nil {
			err = cerr
		}
	}
	return err
}

func (r *RemoteFS) Name() string { return r.name }
func (r *RemoteFS) Remote() bool { return true }

func (r *RemoteFS) ReadDir(p string) ([]Entry, error) {
	fis, err := r.client.ReadDir(p)
	if err != nil {
		return nil, err
	}
	out := make([]Entry, 0, len(fis))
	for _, fi := range fis {
		out = append(out, r.resolve(path.Join(p, fi.Name()), fi))
	}
	return out, nil
}

func (r *RemoteFS) resolve(p string, fi fs.FileInfo) Entry {
	e := entryFromInfo(fi)
	if fi.Mode()&fs.ModeSymlink == 0 {
		return e
	}
	e.Link, _ = r.client.ReadLink(p)
	if target, err := r.client.Stat(p); err == nil {
		e.IsDir, e.Size = target.IsDir(), target.Size()
	} else {
		e.Broken = true
	}
	return e
}

func (r *RemoteFS) Stat(p string) (Entry, error) {
	fi, err := r.client.Lstat(p)
	if err != nil {
		return Entry{}, err
	}
	return r.resolve(p, fi), nil
}

// Open returns the *sftp.File itself so io.Copy can use its concurrent
// WriteTo.
func (r *RemoteFS) Open(p string) (io.ReadCloser, error) { return r.client.Open(p) }

// Create returns the *sftp.File itself so io.Copy can use its concurrent
// ReadFrom.
func (r *RemoteFS) Create(p string) (io.WriteCloser, error) {
	return r.client.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

func (r *RemoteFS) Mkdir(p string) error    { return r.client.Mkdir(p) }
func (r *RemoteFS) MkdirAll(p string) error { return r.client.MkdirAll(p) }

func (r *RemoteFS) Remove(p string) error {
	fi, err := r.client.Lstat(p)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return r.client.RemoveAll(p)
	}
	return r.client.Remove(p)
}

// Rename replaces the destination atomically when the server supports the
// posix-rename extension (OpenSSH does), and falls back to remove+rename.
func (r *RemoteFS) Rename(from, to string) error {
	if _, ok := r.client.HasExtension("posix-rename@openssh.com"); ok {
		return r.client.PosixRename(from, to)
	}
	err := r.client.Rename(from, to)
	if err == nil {
		return nil
	}
	if _, serr := r.client.Lstat(to); serr != nil {
		return err
	}
	if rerr := r.client.Remove(to); rerr != nil {
		return errors.Join(err, rerr)
	}
	return r.client.Rename(from, to)
}

func (r *RemoteFS) Chtimes(p string, mtime time.Time) error {
	return r.client.Chtimes(p, mtime, mtime)
}
func (r *RemoteFS) Chmod(p string, mode fs.FileMode) error { return r.client.Chmod(p, mode) }

func (r *RemoteFS) Home() (string, error) { return r.client.Getwd() }

// RealPath resolves p (e.g. "~/site" or a relative path) on the server.
func (r *RemoteFS) RealPath(p string) (string, error) { return r.client.RealPath(p) }

func (r *RemoteFS) Join(elem ...string) string { return path.Join(elem...) }
func (r *RemoteFS) Dir(p string) string        { return path.Dir(p) }
func (r *RemoteFS) Base(p string) string       { return path.Base(p) }

// Resolve turns a user-supplied remote path into an absolute one: "" and
// "~" are the login directory, "~/x" and relative paths are under it.
func (r *RemoteFS) Resolve(p string) (string, error) {
	if strings.HasPrefix(p, "/") {
		return path.Clean(p), nil
	}
	home, err := r.Home()
	if err != nil {
		return "", err
	}
	p = strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/")
	return path.Join(home, p), nil
}
