package files

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ProductionPanic/haven/v2/internal/sshx"
	"github.com/ProductionPanic/haven/v2/internal/transfer"
	"github.com/ProductionPanic/haven/v2/internal/tui/theme"
	"github.com/ProductionPanic/haven/v2/internal/vfs"
)

// maxEdit is the largest file opened in the editor.
const maxEdit = 20 << 20

// editSession tracks one file being edited.
type editSession struct {
	side   int
	fs     vfs.FS
	path   string // path on fs
	file   string // what the editor opens (path itself for local files)
	tmpDir string // remote only

	// remote file as downloaded, to detect concurrent changes
	size int64
	mod  time.Time
	mode fs.FileMode
	sum  [32]byte
}

func (s *editSession) remote() bool { return s.tmpDir != "" }

func (s *editSession) cleanup() {
	if s.tmpDir != "" {
		os.RemoveAll(s.tmpDir)
	}
}

type (
	editReadyMsg  struct{ s *editSession }
	editBinaryMsg struct {
		side int
		e    vfs.Entry
	}
	editorDoneMsg  struct{ err error }
	editCheckedMsg struct {
		changedRemotely bool
		err             error
	}
	fileCreatedMsg struct {
		side int
		e    vfs.Entry
		err  error
	}
)

// EditorCommand returns the editor argv from $VISUAL or $EDITOR, falling
// back to the first of nvim, vim, vi and nano that is installed.
func EditorCommand() ([]string, error) {
	for _, env := range []string{"VISUAL", "EDITOR"} {
		if v := os.Getenv(env); v != "" {
			args, err := sshx.SplitArgs(v)
			if err == nil && len(args) > 0 {
				return args, nil
			}
		}
	}
	for _, name := range []string{"nvim", "vim", "vi", "nano"} {
		if p, err := exec.LookPath(name); err == nil {
			return []string{p}, nil
		}
	}
	return nil, errors.New("no editor found; set $EDITOR")
}

// execEditor hands the terminal to the editor and reports back when it
// exits.
func execEditor(file string) tea.Cmd {
	args, err := EditorCommand()
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	c := exec.Command(args[0], append(args[1:], file)...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorDoneMsg{err: err} })
}

// prepareEdit checks the file and, for remote files, downloads it to a
// temporary directory. force skips the binary check.
func prepareEdit(side int, fsys vfs.FS, path string, e vfs.Entry, force bool) tea.Cmd {
	return func() tea.Msg {
		if e.Size > maxEdit {
			return editReadyMsg{s: nil} // handled as "too large" below
		}
		r, err := fsys.Open(path)
		if err != nil {
			return opDoneMsg{err: err}
		}
		defer r.Close()

		if !fsys.Remote() {
			head := make([]byte, 8000)
			n, _ := io.ReadFull(r, head)
			if !force && isBinary(head[:n]) {
				return editBinaryMsg{side: side, e: e}
			}
			return editReadyMsg{s: &editSession{side: side, fs: fsys, path: path, file: path}}
		}

		data, err := io.ReadAll(r)
		if err != nil {
			return opDoneMsg{err: err}
		}
		if !force && isBinary(data) {
			return editBinaryMsg{side: side, e: e}
		}
		dir, err := os.MkdirTemp("", "haven-edit-")
		if err != nil {
			return opDoneMsg{err: err}
		}
		// Keep the real name so the editor picks the right syntax.
		file := filepath.Join(dir, fsys.Base(path))
		if err := os.WriteFile(file, data, 0o600); err != nil {
			os.RemoveAll(dir)
			return opDoneMsg{err: err}
		}
		return editReadyMsg{s: &editSession{
			side: side, fs: fsys, path: path, file: file, tmpDir: dir,
			size: e.Size, mod: e.ModTime, mode: e.Mode.Perm(), sum: sha256.Sum256(data),
		}}
	}
}

func fileSum(path string) ([32]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

// checkRemote reports whether the remote file changed since it was
// downloaded.
func checkRemote(s *editSession) tea.Cmd {
	return func() tea.Msg {
		e, err := s.fs.Stat(s.path)
		if errors.Is(err, fs.ErrNotExist) {
			return editCheckedMsg{changedRemotely: true}
		}
		if err != nil {
			return editCheckedMsg{err: err}
		}
		return editCheckedMsg{changedRemotely: e.Size != s.size || !e.ModTime.Equal(s.mod)}
	}
}

// upload writes the edited copy to target and cleans up.
func upload(s *editSession, target string) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(s.file)
		if err != nil {
			return opDoneMsg{err: err}
		}
		if err := transfer.PutFile(s.fs, target, bytes.NewReader(data), s.mode); err != nil {
			return opDoneMsg{err: fmt.Errorf("upload failed, your edit is kept at %s: %w", s.file, err), sides: []int{s.side}}
		}
		s.cleanup()
		return opDoneMsg{what: "Saved " + s.fs.Base(target) + " to " + s.fs.Name(), sides: []int{s.side}, sel: s.fs.Base(target)}
	}
}

func createFile(side int, fsys vfs.FS, path string) tea.Cmd {
	return func() tea.Msg {
		if _, err := fsys.Stat(path); err == nil {
			return fileCreatedMsg{side: side, err: fmt.Errorf("%s already exists", fsys.Base(path))}
		}
		w, err := fsys.Create(path)
		if err == nil {
			err = w.Close()
		}
		if err != nil {
			return fileCreatedMsg{side: side, err: err}
		}
		e, err := fsys.Stat(path)
		return fileCreatedMsg{side: side, e: e, err: err}
	}
}

// --- Model integration ---

// startEdit begins editing the selected entry of the active pane.
func (m *Model) startEdit(side int, e vfs.Entry, force bool) tea.Cmd {
	p := m.panes[side]
	if p.fs == nil || e.IsDir || e.Name == parentName {
		return nil
	}
	if m.editing != nil {
		return m.setStatus("Already editing a file", true)
	}
	if e.Size > maxEdit {
		return m.setStatus(fmt.Sprintf("%s is too large to edit (%s)", e.Name, transfer.FormatBytes(e.Size)), true)
	}
	if p.fs.Remote() {
		m.status, m.statusErr = "Downloading "+e.Name+"…", false
	}
	return prepareEdit(side, p.fs, p.fs.Join(p.cwd, e.Name), e, force)
}

func (m Model) handleEditMsg(msg tea.Msg) (Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case editBinaryMsg:
		side, e := msg.side, msg.e
		m.status = ""
		m.question = &question{
			title: e.Name + " looks like a binary file",
			lines: []string{"Open it in your editor anyway?"},
			options: []qOption{
				{key: "y", label: "open anyway", run: func(m *Model) tea.Cmd { return m.startEdit(side, e, true) }},
				{key: "n", label: "cancel"},
			},
		}
		return m, nil, true

	case editReadyMsg:
		m.status = ""
		if msg.s == nil {
			return m, m.setStatus("File too large to edit", true), true
		}
		m.editing = msg.s
		return m, m.runEditor(msg.s.file), true

	case editorDoneMsg:
		s := m.editing
		if s == nil {
			return m, nil, true
		}
		if msg.err != nil {
			m.editing = nil
			s.cleanup()
			return m, m.setStatus("Editor: "+msg.err.Error(), true), true
		}
		if !s.remote() {
			m.editing = nil
			return m, m.list(s.side, m.panes[s.side].cwd, s.fs.Base(s.path)), true
		}
		sum, err := fileSum(s.file)
		if err != nil {
			m.editing = nil
			return m, m.setStatus(err.Error(), true), true
		}
		if sum == s.sum {
			m.editing = nil
			s.cleanup()
			return m, m.setStatus("No changes to "+s.fs.Base(s.path), false), true
		}
		m.status, m.statusErr = "Checking "+s.fs.Base(s.path)+" on "+m.cfg.Host.Name+"…", false
		return m, checkRemote(s), true

	case editCheckedMsg:
		s := m.editing
		if s == nil {
			return m, nil, true
		}
		m.status = ""
		if msg.err != nil {
			m.editing = nil
			return m, m.setStatus(fmt.Sprintf("%v (your edit is kept at %s)", msg.err, s.file), true), true
		}
		name := s.fs.Base(s.path)
		discard := qOption{key: "d", label: "don't upload", run: func(m *Model) tea.Cmd {
			m.editing = nil
			return m.setStatus("Not uploaded; your edit is kept at "+s.file, false)
		}}
		if msg.changedRemotely {
			m.question = &question{
				title:  name + " changed on " + m.cfg.Host.Name + " while you were editing",
				lines:  []string{"Someone (or something) modified or removed it after you opened it."},
				border: m.theme.Warning,
				options: []qOption{
					{key: "o", label: "overwrite theirs", run: func(m *Model) tea.Cmd { m.editing = nil; return upload(s, s.path) }},
					{key: "k", label: "keep both", run: func(m *Model) tea.Cmd {
						m.editing = nil
						return upload(s, transfer.FreeName(s.fs, s.path))
					}},
					discard,
				},
			}
			return m, nil, true
		}
		if theme.IsProduction(m.cfg.Host.Environment) {
			m.question = &question{
				title:  "Upload " + name + " to " + m.cfg.Host.Name + "?",
				lines:  []string{"This is a PRODUCTION host."},
				border: m.theme.Danger,
				options: []qOption{
					{key: "y", label: "upload", run: func(m *Model) tea.Cmd { m.editing = nil; return upload(s, s.path) }},
					discard,
				},
			}
			return m, nil, true
		}
		m.editing = nil
		return m, upload(s, s.path), true

	case fileCreatedMsg:
		if msg.err != nil {
			return m, m.setStatus(msg.err.Error(), true), true
		}
		p := m.panes[msg.side]
		return m, tea.Batch(m.list(msg.side, p.cwd, msg.e.Name), m.startEdit(msg.side, msg.e, true)), true
	}
	return m, nil, false
}
