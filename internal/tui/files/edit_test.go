package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/store"
)

// fakeEditor replaces the editor with f, run synchronously on the file the
// editor would open. It records the opened files.
func fakeEditor(e *env, f func(path string)) *[]string {
	var opened []string
	e.m.runEditor = func(file string) tea.Cmd {
		opened = append(opened, file)
		return func() tea.Msg {
			if f != nil {
				f(file)
			}
			return editorDoneMsg{}
		}
	}
	return &opened
}

func appendText(s string) func(string) {
	return func(p string) {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
		f.WriteString(s)
		f.Close()
	}
}

func TestViewer(t *testing.T) {
	e := setup(t, store.Host{})
	mk(t, filepath.Join(e.local, "notes.txt"), "alpha\nfoo one\nbeta\nFOO two\n")
	e.keys("ctrl+r", "G", "enter") // notes.txt sorts last
	if e.m.viewer == nil {
		t.Fatal("viewer not open")
	}
	v := e.view()
	for _, want := range []string{"notes.txt", "4 lines", "1  alpha", "4  FOO two"} {
		if !strings.Contains(v, want) {
			t.Errorf("viewer missing %q:\n%s", want, v)
		}
	}
	e.keys("/", "foo", "enter")
	if !strings.Contains(e.view(), "match 1/2") {
		t.Errorf("search:\n%s", e.view())
	}
	e.keys("n")
	if !strings.Contains(e.view(), "match 2/2") {
		t.Errorf("next match:\n%s", e.view())
	}
	e.keys("esc") // clears search
	e.keys("q")
	if e.m.viewer != nil {
		t.Error("q should close the viewer")
	}
}

func TestViewerRemoteHighlightAndBinary(t *testing.T) {
	e := setup(t, store.Host{})
	mk(t, filepath.Join(e.remote, "site", "logo.png"), "\x89PNG\x00\x00binary")
	e.keys("tab", "ctrl+r", "G", "enter") // wp-config.php is last
	if !strings.Contains(e.view(), "1  config") {
		t.Errorf("remote file not shown:\n%s", e.view())
	}
	e.keys("q", "k", "enter") // logo.png
	if !strings.Contains(e.view(), "Binary file") {
		t.Errorf("binary not detected:\n%s", e.view())
	}
}

func TestHighlightLines(t *testing.T) {
	src := "<?php\n/* multi\nline */\necho 'hi';"
	lines := highlightLines("x.php", src, true)
	if len(lines) != 4 {
		t.Fatalf("got %d lines", len(lines))
	}
	for i, l := range lines {
		if !strings.Contains(l, "\x1b[") {
			t.Errorf("line %d not coloured: %q", i, l)
		}
		// Every line must end with colours reset, so lines are independent.
		if strings.Count(l, "\x1b[0m") == 0 {
			t.Errorf("line %d leaks colour: %q", i, l)
		}
	}
	if highlightLines("x.unknownext", "plain words here", true) != nil {
		t.Log("analyser picked a lexer for plain text (fine)")
	}
}

func TestEditLocal(t *testing.T) {
	e := setup(t, store.Host{})
	opened := fakeEditor(e, appendText("\necho 1;"))
	e.keys("G", "e") // index.php
	if len(*opened) != 1 || (*opened)[0] != filepath.Join(e.local, "index.php") {
		t.Fatalf("editor opened %v", *opened)
	}
	if got := mustRead(t, filepath.Join(e.local, "index.php")); got != "<?php\necho 1;" {
		t.Errorf("content %q", got)
	}
}

func TestEditRemote(t *testing.T) {
	e := setup(t, store.Host{})
	remoteFile := filepath.Join(e.remote, "site", "wp-config.php")
	opened := fakeEditor(e, appendText("\ndefine('WP_DEBUG', true);"))
	e.keys("tab", "G", "e")
	if len(*opened) != 1 || (*opened)[0] == remoteFile || filepath.Base((*opened)[0]) != "wp-config.php" {
		t.Fatalf("editor should open a local temp copy named wp-config.php, got %v", *opened)
	}
	if got := mustRead(t, remoteFile); got != "config\ndefine('WP_DEBUG', true);" {
		t.Errorf("remote content %q", got)
	}
	if _, err := os.Stat(filepath.Dir((*opened)[0])); !os.IsNotExist(err) {
		t.Error("temp dir not cleaned up")
	}
	if !strings.Contains(e.view(), "Saved wp-config.php") {
		t.Errorf("status:\n%s", e.view())
	}
}

func TestEditRemoteUnchanged(t *testing.T) {
	e := setup(t, store.Host{})
	remoteFile := filepath.Join(e.remote, "site", "wp-config.php")
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	os.Chtimes(remoteFile, old, old)
	fakeEditor(e, nil)
	e.keys("tab", "ctrl+r", "G", "e")
	if !strings.Contains(e.view(), "No changes") {
		t.Errorf("status:\n%s", e.view())
	}
	if fi, _ := os.Stat(remoteFile); !fi.ModTime().Equal(old) {
		t.Error("unchanged file was re-uploaded")
	}
}

func TestEditRemoteChangedMeanwhile(t *testing.T) {
	e := setup(t, store.Host{})
	remoteFile := filepath.Join(e.remote, "site", "wp-config.php")
	fakeEditor(e, func(p string) {
		appendText(" mine")(p)
		// Someone else saves the server copy while we edit.
		os.WriteFile(remoteFile, []byte("theirs"), 0o644)
		future := time.Now().Add(time.Hour)
		os.Chtimes(remoteFile, future, future)
	})
	e.keys("tab", "G", "e")
	if e.m.question == nil || !strings.Contains(e.view(), "changed on test.nl while") {
		t.Fatalf("no conflict question:\n%s", e.view())
	}
	e.keys("k") // keep both
	if mustRead(t, remoteFile) != "theirs" {
		t.Error("their version was overwritten")
	}
	if got := mustRead(t, filepath.Join(e.remote, "site", "wp-config (1).php")); got != "config mine" {
		t.Errorf("our copy = %q", got)
	}
}

func TestEditProductionConfirm(t *testing.T) {
	e := setup(t, store.Host{Name: "prod.nl", Environment: "production"})
	remoteFile := filepath.Join(e.remote, "site", "wp-config.php")
	opened := fakeEditor(e, appendText(" edited"))
	e.keys("tab", "G", "e")
	if e.m.question == nil || !strings.Contains(e.view(), "PRODUCTION") {
		t.Fatalf("no production confirm:\n%s", e.view())
	}
	e.keys("d") // don't upload
	if mustRead(t, remoteFile) != "config" {
		t.Error("uploaded despite declining")
	}
	if got := mustRead(t, (*opened)[0]); got != "config edited" {
		t.Errorf("edited copy not kept: %q", got)
	}
	if !strings.Contains(e.view(), "kept at") {
		t.Errorf("status:\n%s", e.view())
	}

	// And accepting uploads.
	e.keys("e", "y")
	if mustRead(t, remoteFile) != "config edited" {
		t.Errorf("not uploaded after confirming: %q", mustRead(t, remoteFile))
	}
}

func TestNewFile(t *testing.T) {
	e := setup(t, store.Host{})
	opened := fakeEditor(e, appendText("hello"))
	e.keys("n", "todo.md", "enter")
	if got := mustRead(t, filepath.Join(e.local, "todo.md")); got != "hello" {
		t.Errorf("local new file = %q", got)
	}
	if sel, _ := e.m.panes[left].selected(); sel.Name != "todo.md" {
		t.Errorf("new file not selected: %q", sel.Name)
	}

	// Remote, and refusing to clobber an existing name.
	e.keys("tab", "n", "notes.txt", "enter")
	if got := mustRead(t, filepath.Join(e.remote, "site", "notes.txt")); got != "hello" {
		t.Errorf("remote new file = %q", got)
	}
	before := len(*opened)
	e.keys("n", "wp-config.php", "enter")
	if len(*opened) != before || !strings.Contains(e.view(), "already exists") {
		t.Errorf("existing name should be refused:\n%s", e.view())
	}
}

func TestEditBinaryAsks(t *testing.T) {
	e := setup(t, store.Host{})
	mk(t, filepath.Join(e.local, "zz.bin"), "a\x00b")
	opened := fakeEditor(e, nil)
	e.keys("ctrl+r", "G", "e")
	if e.m.question == nil || len(*opened) != 0 {
		t.Fatalf("binary should ask first:\n%s", e.view())
	}
	e.keys("y")
	if len(*opened) != 1 {
		t.Error("editor not opened after confirming")
	}
}
