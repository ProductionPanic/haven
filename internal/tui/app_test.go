package tui

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ProductionPanic/haven/v2/internal/store"
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

	d, _ := setupWith(t, s, func(context.Context, []string) error { return nil })
	return d, s
}

// setupWith starts the app on s with run standing in for ssh, and returns
// the hostnames run was called for.
func setupWith(t *testing.T, s *store.Store, run func(context.Context, []string) error) (*driver, *calls) {
	t.Helper()
	c := &calls{}
	app := New(context.Background(), s)
	app.checkRun = func(ctx context.Context, args []string) error {
		c.add(args[len(args)-2])
		return run(ctx, args)
	}
	d := &driver{t: t, app: app}
	d.send(tea.WindowSizeMsg{Width: 120, Height: 40})
	d.send(d.app.Init()())
	return d, c
}

type calls struct {
	mu      sync.Mutex
	targets []string
}

func (c *calls) add(target string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.targets = append(c.targets, target)
}

func (c *calls) list() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := append([]string(nil), c.targets...)
	sort.Strings(out)
	return out
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

func checkOf(t *testing.T, s *store.Store, name string) *store.Check {
	t.Helper()
	h, err := s.Get(context.Background(), name)
	if err != nil {
		t.Fatal(err)
	}
	return h.Check
}

func TestChecksOnOpen(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	s.Create(ctx, store.Host{Name: "alpha.nl", User: "u", Hostname: "a"})
	s.Create(ctx, store.Host{Name: "beta.nl", User: "u", Hostname: "down"})
	fresh, _ := s.Create(ctx, store.Host{Name: "fresh.nl", User: "u", Hostname: "f"})
	s.SaveCheck(ctx, fresh.ID, store.Check{At: time.Now(), OK: true})

	d, c := setupWith(t, s, func(_ context.Context, args []string) error {
		if strings.Contains(args[len(args)-2], "down") {
			return errors.New("Connection refused")
		}
		return nil
	})
	if got := c.list(); !reflect.DeepEqual(got, []string{"u@a", "u@down"}) {
		t.Fatalf("checked %v, want only the stale hosts", got)
	}
	if ch := checkOf(t, s, "alpha.nl"); ch == nil || !ch.OK {
		t.Errorf("alpha check = %+v, want ok", ch)
	}
	if ch := checkOf(t, s, "beta.nl"); ch == nil || ch.OK || ch.Error != "Connection refused" {
		t.Errorf("beta check = %+v, want failed", ch)
	}

	// Reloading (after an edit, a shell, ...) does not check again.
	d.send(d.app.(App).load(-1)())
	if got := c.list(); len(got) != 2 {
		t.Errorf("reload checked again: %v", got)
	}

	d.keys("j") // beta.nl
	view := ansi.Strip(d.app.View().Content)
	if !strings.Contains(view, "SSH check") || !strings.Contains(view, "failed") {
		t.Errorf("detail panel misses the failed check:\n%s", view)
	}
}

func TestCheckKeys(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	for _, name := range []string{"alpha.nl", "beta.nl"} {
		h, _ := s.Create(ctx, store.Host{Name: name, User: "u", Hostname: name})
		s.SaveCheck(ctx, h.ID, store.Check{At: time.Now(), OK: true})
	}
	d, c := setupWith(t, s, func(context.Context, []string) error { return nil })
	if got := c.list(); len(got) != 0 {
		t.Fatalf("fresh hosts were checked on open: %v", got)
	}
	d.keys("p")
	if got := c.list(); !reflect.DeepEqual(got, []string{"u@alpha.nl"}) {
		t.Fatalf("p checked %v, want the selected host", got)
	}
	d.keys("P")
	if got := c.list(); !reflect.DeepEqual(got, []string{"u@alpha.nl", "u@alpha.nl", "u@beta.nl"}) {
		t.Fatalf("P checked %v, want every host", got)
	}
}
