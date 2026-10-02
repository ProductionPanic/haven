package tui

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/store"
)

// driver feeds messages to the app and runs the resulting commands, so
// tests exercise the same loop as tea.Program (minus rendering).
type driver struct {
	t   *testing.T
	app tea.Model
}

func (d *driver) send(msg tea.Msg) {
	d.t.Helper()
	queue := []tea.Msg{msg}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 200 {
			d.t.Fatal("message loop did not settle")
		}
		m := queue[0]
		queue = queue[1:]
		if batch, ok := m.(tea.BatchMsg); ok {
			for _, c := range batch {
				if r := run(c); r != nil {
					queue = append(queue, r)
				}
			}
			continue
		}
		var cmd tea.Cmd
		d.app, cmd = d.app.Update(m)
		if r := run(cmd); r != nil {
			queue = append(queue, r)
		}
	}
}

// run executes cmd, dropping commands that block (ticks, blinks) and
// internal tea messages the program would normally handle.
func run(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		switch msg.(type) {
		case tea.BatchMsg, nil:
			return msg
		}
		if reflect.TypeOf(msg).PkgPath() == "charm.land/bubbletea/v2" {
			return nil // quit, clipboard, background-colour requests, ...
		}
		return msg
	case <-time.After(30 * time.Millisecond):
		return nil
	}
}

func (d *driver) keys(keys ...string) {
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		default:
			for _, r := range k {
				d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
			}
			continue
		}
		d.send(msg)
	}
}

func setup(t *testing.T) (*driver, *store.Store) {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.Create(ctx, store.Host{Name: "alpha.nl", User: "u", Hostname: "a"})
	s.Create(ctx, store.Host{Name: "beta.nl", User: "u", Hostname: "b", Environment: "production"})

	d := &driver{t: t, app: New(ctx, s)}
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	d.send(d.app.Init()())
	return d, s
}

func names(t *testing.T, s *store.Store) []string {
	hs, err := s.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, h := range hs {
		out = append(out, h.Name)
	}
	return out
}

func TestDeleteConfirm(t *testing.T) {
	d, s := setup(t)
	d.keys("d")
	if d.app.(App).confirm == nil {
		t.Fatal("confirm dialog not open")
	}
	d.keys("enter") // default is Cancel
	if got := names(t, s); len(got) != 2 {
		t.Fatalf("enter on default should cancel, hosts = %v", got)
	}
	d.keys("d", "y")
	if got := names(t, s); !reflect.DeepEqual(got, []string{"beta.nl"}) {
		t.Fatalf("after delete: %v", got)
	}
}

func TestAddViaForm(t *testing.T) {
	d, s := setup(t)
	d.keys("a")
	if d.app.(App).form == nil {
		t.Fatal("form not open")
	}
	d.keys("gamma.nl", "enter", "deploy", "enter", "srv3", "enter", "2200", "enter",
		"enter", // environment: none
		"enter", // tags
		"enter", // remote path
		"enter", // identity
		"enter", // jump host
		"enter", // extra args
		"enter") // notes → submit
	if d.app.(App).form != nil {
		t.Fatal("form still open")
	}
	h, err := s.Get(context.Background(), "gamma.nl")
	if err != nil {
		t.Fatalf("host not created: %v (status %q)", err, d.app.(App).status)
	}
	if h.Target() != "deploy@srv3" || h.Port != 2200 {
		t.Errorf("unexpected host %+v", h)
	}
}

func TestEditCancel(t *testing.T) {
	d, s := setup(t)
	d.keys("e", "X", "esc")
	if d.app.(App).form != nil {
		t.Fatal("esc should close the form")
	}
	if got := names(t, s); !reflect.DeepEqual(got, []string{"alpha.nl", "beta.nl"}) {
		t.Errorf("cancelled edit changed hosts: %v", got)
	}
}

func TestConnectResult(t *testing.T) {
	d, _ := setup(t)
	d.keys("j", "enter")
	if h := d.app.(App).connectHost; h == nil || h.Name != "beta.nl" {
		t.Fatalf("connectHost = %+v", h)
	}
}

func TestFilesConfigStartsInCwd(t *testing.T) {
	_, s := setup(t)
	ctx := context.Background()
	h, _ := s.Get(ctx, "alpha.nl")
	s.SaveDirs(ctx, h.ID, t.TempDir(), "/var/www/site")

	cwd := t.TempDir()
	t.Chdir(cwd)
	cfg := FilesConfig(ctx, s, h)
	if cfg.LocalDir != cwd {
		t.Errorf("LocalDir = %q, want current directory %q", cfg.LocalDir, cwd)
	}
	if cfg.RemoteDir != "/var/www/site" {
		t.Errorf("RemoteDir = %q, want the remembered one", cfg.RemoteDir)
	}
}
