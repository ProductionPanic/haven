package hosts

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/store"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui/theme"
)

var now = time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)

func testHosts() []store.Host {
	used := now.Add(-3 * time.Minute)
	return []store.Host{
		{ID: 1, Name: "appelenburg.nl", User: "web", Hostname: "monotone", Port: 22, Environment: "staging",
			Tags: []string{"client-a", "wordpress"}, Notes: "DB creds in 1Password", UseCount: 1, LastUsedAt: &used},
		{ID: 2, Name: "formhost", User: "deploy", Hostname: "10.0.0.5", Port: 2022, RemotePath: "/srv/app",
			Environment: "production", Tags: []string{"wp"}},
		{ID: 3, Name: "foo.nl", User: "foo", Hostname: "bar.io", Port: 22},
		{ID: 4, Name: "foobar.com", User: "fb", Hostname: "bar.io", Port: 22, Tags: []string{"wp"}},
		{ID: 5, Name: "other.nl", User: "web", Hostname: "monotone", Port: 22, Tags: []string{"wordpress"}},
	}
}

func newModel(t *testing.T) Model {
	t.Helper()
	m := New(theme.New(true))
	m.now = func() time.Time { return now }
	m.SetSize(120, 30)
	m.SetHosts(testHosts())
	return m
}

func press(m Model, keys ...string) (Model, []tea.Msg) {
	var msgs []tea.Msg
	for _, k := range keys {
		var msg tea.KeyPressMsg
		switch k {
		case "enter":
			msg = tea.KeyPressMsg{Code: tea.KeyEnter}
		case "esc":
			msg = tea.KeyPressMsg{Code: tea.KeyEscape}
		case "down":
			msg = tea.KeyPressMsg{Code: tea.KeyDown}
		case "up":
			msg = tea.KeyPressMsg{Code: tea.KeyUp}
		default:
			r := []rune(k)[0]
			msg = tea.KeyPressMsg{Code: r, Text: k}
		}
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if msg := runCmd(cmd); msg != nil {
			msgs = append(msgs, msg)
		}
	}
	return m, msgs
}

// runCmd runs cmd, ignoring commands that block (like cursor blink ticks).
func runCmd(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(20 * time.Millisecond):
		return nil
	}
}

func plain(m Model) string { return ansi.Strip(m.View()) }

func selectedName(m Model) string {
	h, _ := m.Selected()
	return h.Name
}

func TestNavigationAndDetail(t *testing.T) {
	m := newModel(t)
	// appelenburg was used recently, so frecency puts it first.
	if got := selectedName(m); got != "appelenburg.nl" {
		t.Fatalf("initial selection %q", got)
	}
	m, _ = press(m, "j")
	if got := selectedName(m); got != "foo.nl" {
		t.Fatalf("after j: %q", got)
	}
	m, _ = press(m, "j", "j")
	if got := selectedName(m); got != "formhost" {
		t.Fatalf("after jjj: %q", got)
	}
	v := plain(m)
	for _, want := range []string{"PRODUCTION", "Port       2022", "Path       /srv/app", "Last used  never",
		"$ ssh -t -p 2022 deploy@10.0.0.5 'cd /srv/app && exec"} {
		if !strings.Contains(v, want) {
			t.Errorf("detail missing %q:\n%s", want, v)
		}
	}
	if strings.Contains(v, "DB creds") {
		t.Errorf("stale notes from previous host:\n%s", v)
	}
}

func TestFilterAndConnect(t *testing.T) {
	m := newModel(t)
	m, _ = press(m, "/", "f", "o", "o")
	if !m.Filtering() {
		t.Fatal("filter should be focused")
	}
	if v := plain(m); !strings.Contains(v, "2 of 5 hosts") {
		t.Errorf("filter count:\n%s", v)
	}
	// j while filtering types into the filter instead of moving.
	m, _ = press(m, "j")
	if v := plain(m); !strings.Contains(v, "0 of 5 hosts") {
		t.Errorf("j should be typed while filtering:\n%s", v)
	}
	m, _ = press(m, "esc") // blur
	m, _ = press(m, "esc") // clear
	if v := plain(m); !strings.Contains(v, "5 of 5 hosts") {
		t.Errorf("esc should clear the filter:\n%s", v)
	}
	m, _ = press(m, "/", "o", "t", "h")
	_, msgs := press(m, "enter")
	if len(msgs) != 1 {
		t.Fatalf("enter produced %v", msgs)
	}
	if c, ok := msgs[0].(ConnectMsg); !ok || c.Host.Name != "other.nl" {
		t.Errorf("enter produced %#v", msgs[0])
	}
}

func TestTagsAndGrouping(t *testing.T) {
	m := newModel(t)
	m, _ = press(m, "t") // client-a
	if v := plain(m); !strings.Contains(v, "1 of 5 hosts · #client-a") {
		t.Errorf("tag filter:\n%s", v)
	}
	m, _ = press(m, "t", "t") // wordpress, then wp
	if v := plain(m); !strings.Contains(v, "2 of 5 hosts · #wp") {
		t.Errorf("tag cycle:\n%s", v)
	}
	m, _ = press(m, "t", "g") // no tag, grouped
	v := plain(m)
	for _, want := range []string{"grouped by server", "bar.io (2)", "monotone (2)", "10.0.0.5 (1)"} {
		if !strings.Contains(v, want) {
			t.Errorf("grouped view missing %q:\n%s", want, v)
		}
	}
	// Cursor never lands on a header.
	for i := 0; i < 10; i++ {
		m, _ = press(m, "j")
		if _, ok := m.Selected(); !ok {
			t.Fatal("cursor on a header row")
		}
	}
}

func TestActions(t *testing.T) {
	m := newModel(t)
	cases := map[string]func(tea.Msg) bool{
		"a": func(msg tea.Msg) bool { _, ok := msg.(AddMsg); return ok },
		"e": func(msg tea.Msg) bool { e, ok := msg.(EditMsg); return ok && e.Host.ID == 1 },
		"d": func(msg tea.Msg) bool { d, ok := msg.(DeleteMsg); return ok && d.Host.ID == 1 },
		"y": func(msg tea.Msg) bool { c, ok := msg.(CopyMsg); return ok && c.Host.Target() == "web@monotone" },
		"s": func(msg tea.Msg) bool { s, ok := msg.(ShellMsg); return ok && s.Host.ID == 1 },
		"f": func(msg tea.Msg) bool { f, ok := msg.(FilesMsg); return ok && f.Host.ID == 1 },
		"q": func(msg tea.Msg) bool { _, ok := msg.(QuitMsg); return ok },
	}
	for k, check := range cases {
		_, msgs := press(m, k)
		if len(msgs) != 1 || !check(msgs[0]) {
			t.Errorf("key %q produced %#v", k, msgs)
		}
	}
}

func TestEmpty(t *testing.T) {
	m := New(theme.New(false))
	m.SetSize(80, 20)
	m.SetHosts(nil)
	if v := plain(m); !strings.Contains(v, "No hosts yet") {
		t.Errorf("empty view:\n%s", v)
	}
	if _, msgs := press(m, "enter", "e", "d"); len(msgs) != 0 {
		t.Errorf("actions on empty list produced %v", msgs)
	}
}
