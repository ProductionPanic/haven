package transfer

import (
	"io"
	"io/fs"

	"github.com/ProductionPanic/haven/v2/internal/vfs"
)

// PutFile writes r to path on fsys atomically: the data goes to a
// .haven-part file that replaces path only once fully written. mode is
// applied when non-zero.
func PutFile(fsys vfs.FS, path string, r io.Reader, mode fs.FileMode) error {
	part := path + PartSuffix
	w, err := fsys.Create(part)
	if err != nil {
		return err
	}
	_, err = io.Copy(w, r)
	if cerr := w.Close(); err == nil {
		err = cerr
	}
	if err == nil && mode != 0 {
		err = fsys.Chmod(part, mode.Perm())
	}
	if err == nil {
		err = fsys.Rename(part, path)
	}
	if err != nil {
		_ = fsys.Remove(part)
	}
	return err
}
