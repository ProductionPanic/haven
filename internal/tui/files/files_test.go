package files

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ProductionPanic/rootnet-cli/internal/store"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/theme"
	"github.com/ProductionPanic/rootnet-cli/internal/vfs"
	"github.com/ProductionPanic/rootnet-cli/internal/vfs/vfstest"
)

// driver runs the model like tea.Program would: commands execute in
// goroutines and their messages are fed back until things settle.
type driver struct {
	t    *testing.T
	m    Model
	msgs chan tea.Msg
}

func (d *driver) exec(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	go func() {
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				d.exec(c)
			}
			return
		}
		if msg != nil {
			d.msgs <- msg
		}
	}()
}

func (d *driver) settle() {
	d.t.Helper()
	for {
		select {
		case msg := <-d.msgs:
			var cmd tea.Cmd
			d.m, cmd = d.m.Update(msg)
			d.exec(cmd)
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}

func (d *driver) keys(keys ...string) {
	d.t.Helper()
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "tab":
			msg = tea.KeyPressMsg{Code: tea.KeyTab}
		case "space":
			msg = tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
		case "backspace":
			msg = tea.KeyPressMsg{Code: tea.KeyBackspace}
		case "ctrl+r":
			msg = tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
		default:
			for _, r := range k {
				var cmd tea.Cmd
				d.m, cmd = d.m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
				d.exec(cmd)
			}
			d.settle()
			continue
		}
		var cmd tea.Cmd
		d.m, cmd = d.m.Update(msg)
		d.exec(cmd)
		d.settle()
	}
}

func (d *driver) view() string { return ansi.Strip(d.m.View()) }

func mk(t *testing.T, p, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

type env struct {
	*driver
	local, remote string
	closed        *CloseMsg
}

func setup(t *testing.T, h store.Host) *env {
	t.Helper()
	local, remote := t.TempDir(), t.TempDir()
	mk(t, filepath.Join(local, "index.php"), "<?php")
	mk(t, filepath.Join(local, "wp-content", "a.css"), "body{}")
	mk(t, filepath.Join(local, ".env"), "SECRET")
	mk(t, filepath.Join(remote, "site", "wp-config.php"), "config")

	rfs := vfstest.NewRemote(t)
	if h.Name == "" {
		h = store.Host{Name: "test.nl", Hostname: "x"}
	}
	h.RemotePath = filepath.Join(remote, "site")
	cfg := Config{
		Host:     h,
		Dial:     func(context.Context, store.Host) (*vfs.RemoteFS, error) { return rfs, nil },
		LocalDir: local,
		Workers:  2,
	}
	d := &driver{t: t, m: New(context.Background(), theme.New(true), cfg), msgs: make(chan tea.Msg, 100)}
	d.m.now = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
	d.m.SetSize(120, 30)
	d.exec(d.m.Init())
	d.settle()
	return &env{driver: d, local: local, remote: remote}
}

func names(p *pane) []string {
	var out []string
	for _, e := range p.visible {
		out = append(out, e.Name)
	}
	return out
}

func TestBrowse(t *testing.T) {
	e := setup(t, store.Host{})
	l, r := e.m.panes[left], e.m.panes[right]
	if !reflect.DeepEqual(names(l), []string{"..", "wp-content", "index.php"}) {
		t.Fatalf("local listing %v", names(l))
	}
	if r.cwd != filepath.Join(e.remote, "site") || !reflect.DeepEqual(names(r), []string{"..", "wp-config.php"}) {
		t.Fatalf("remote listing %s %v", r.cwd, names(r))
	}
	v := e.view()
	for _, want := range []string{"Local: ", "test.nl: " + filepath.Join(e.remote, "site"), "wp-content/", "wp-config.php"} {
		if !strings.Contains(v, want) {
			t.Errorf("view missing %q:\n%s", want, v)
		}
	}

	e.keys(".") // show hidden
	if !reflect.DeepEqual(names(l), []string{"..", "wp-content", ".env", "index.php"}) {
		t.Errorf("with hidden: %v", names(l))
	}
	e.keys("j", "enter") // into wp-content
	if l.cwd != filepath.Join(e.local, "wp-content") {
		t.Fatalf("cwd after enter: %s", l.cwd)
	}
	e.keys("backspace")
	if sel, _ := l.selected(); l.cwd != e.local || sel.Name != "wp-content" {
		t.Errorf("parent: cwd %s, selected %q", l.cwd, sel.Name)
	}
	e.keys("/", "ind", "enter")
	if !reflect.DeepEqual(names(l), []string{"..", "index.php"}) {
		t.Errorf("filtered: %v", names(l))
	}
	e.keys("esc")
	if len(names(l)) != 4 {
		t.Errorf("esc should clear the filter: %v", names(l))
	}
}

func TestCopyBothWays(t *testing.T) {
	e := setup(t, store.Host{})
	// Mark wp-content and index.php, upload.
	e.keys("j", "space", "space", "c")
	if got := mustRead(t, filepath.Join(e.remote, "site", "wp-content", "a.css")); got != "body{}" {
		t.Errorf("uploaded a.css = %q", got)
	}
	if mustRead(t, filepath.Join(e.remote, "site", "index.php")) != "<?php" {
		t.Error("index.php not uploaded")
	}
	if !strings.Contains(e.view(), "Copied 2 file(s)") {
		t.Errorf("status:\n%s", e.view())
	}
	if !reflect.DeepEqual(names(e.m.panes[right]), []string{"..", "wp-content", "index.php", "wp-config.php"}) {
		t.Errorf("remote pane not refreshed: %v", names(e.m.panes[right]))
	}

	// Download wp-config.php.
	e.keys("tab", "G", "c")
	if mustRead(t, filepath.Join(e.local, "wp-config.php")) != "config" {
		t.Error("download failed")
	}
}

func TestConflictDialog(t *testing.T) {
	e := setup(t, store.Host{Name: "prod.nl", Environment: "production"})
	mk(t, filepath.Join(e.remote, "site", "index.php"), "old")
	e.keys("ctrl+r")
	e.keys("G", "c") // index.php
	if e.m.conflict == nil {
		t.Fatalf("no conflict dialog:\n%s", e.view())
	}
	v := e.view()
	for _, want := range []string{"File already exists", "PRODUCTION", "o overwrite"} {
		if !strings.Contains(v, want) {
			t.Errorf("dialog missing %q:\n%s", want, v)
		}
	}
	e.keys("s")
	if mustRead(t, filepath.Join(e.remote, "site", "index.php")) != "old" {
		t.Error("skip overwrote the file")
	}
	e.keys("c", "o")
	if mustRead(t, filepath.Join(e.remote, "site", "index.php")) != "<?php" {
		t.Error("overwrite didn't happen")
	}
}

func TestMkdirRenameDelete(t *testing.T) {
	e := setup(t, store.Host{})
	e.keys("tab", "m", "uploads", "enter")
	r := e.m.panes[right]
	if fi, err := os.Stat(filepath.Join(r.cwd, "uploads")); err != nil || !fi.IsDir() {
		t.Fatalf("mkdir: %v", err)
	}
	if sel, _ := r.selected(); sel.Name != "uploads" {
		t.Errorf("new dir not selected: %q", sel.Name)
	}

	e.keys("r")
	e.m.input.SetValue("")
	e.keys("media", "enter")
	if _, err := os.Stat(filepath.Join(r.cwd, "media")); err != nil {
		t.Fatalf("rename: %v", err)
	}

	e.keys("d")
	if e.m.confirmDelete == nil || !strings.Contains(e.view(), "Delete media?") {
		t.Fatalf("no delete confirm:\n%s", e.view())
	}
	e.keys("n")
	if _, err := os.Stat(filepath.Join(r.cwd, "media")); err != nil {
		t.Fatal("n should not delete")
	}
	e.keys("d", "y")
	if _, err := os.Stat(filepath.Join(r.cwd, "media")); err == nil {
		t.Fatal("not deleted")
	}
}

func TestQuitReportsDirs(t *testing.T) {
	e := setup(t, store.Host{})
	var cmd tea.Cmd
	e.m, cmd = e.m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	msg, ok := cmd().(CloseMsg)
	if !ok || msg.LocalDir != e.local || msg.RemoteDir != filepath.Join(e.remote, "site") {
		t.Fatalf("close msg %+v", msg)
	}
}

func TestConnectError(t *testing.T) {
	d := &driver{t: t, msgs: make(chan tea.Msg, 10)}
	d.m = New(context.Background(), theme.New(true), Config{
		Host: store.Host{Name: "down.nl"},
		Dial: func(context.Context, store.Host) (*vfs.RemoteFS, error) {
			return nil, os.ErrDeadlineExceeded
		},
		LocalDir: t.TempDir(),
	})
	d.m.SetSize(100, 20)
	d.exec(d.m.Init())
	d.settle()
	if v := d.view(); !strings.Contains(v, "Could not connect") {
		t.Errorf("no error shown:\n%s", v)
	}
	d.keys("tab", "c", "m") // must not crash without a remote
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}
