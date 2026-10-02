// Package tui is rootnet's full-screen host manager. The App routes between
// the hosts screen and its overlays (host form, confirm dialog).
package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"

	"github.com/ProductionPanic/rootnet-cli/internal/sshx"
	"github.com/ProductionPanic/rootnet-cli/internal/store"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/hostform"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/hosts"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/theme"
)

// Store is the subset of *store.Store the app needs.
type Store interface {
	List(ctx context.Context, tag string) ([]store.Host, error)
	Get(ctx context.Context, name string) (store.Host, error)
	Create(ctx context.Context, h store.Host) (store.Host, error)
	Update(ctx context.Context, h store.Host) (store.Host, error)
	Delete(ctx context.Context, id int64) error
	MarkUsed(ctx context.Context, id int64) error
}

type (
	hostsLoadedMsg struct {
		hosts    []store.Host
		selectID int64
	}
	statusMsg struct {
		text  string
		isErr bool
	}
	clearStatusMsg struct{ seq int }
	formDoneMsg    struct{}
	shellDoneMsg   struct {
		host store.Host
		err  error
	}
)

// App is the root model.
type App struct {
	ctx   context.Context
	store Store
	theme theme.Theme

	hosts hosts.Model

	form      *huh.Form
	fields    *hostform.Fields
	editing   store.Host
	confirm   *confirm
	pendingRm store.Host

	status      string
	statusErr   bool
	statusSeq   int
	width       int
	height      int
	connectHost *store.Host
}

// New creates the app.
func New(ctx context.Context, s Store) App {
	t := theme.New(true)
	return App{ctx: ctx, store: s, theme: t, hosts: hosts.New(t)}
}

// Run starts the host manager. It returns the host to connect to when the
// user chose one with enter, or nil when they quit.
func Run(ctx context.Context, s Store) (*store.Host, error) {
	final, err := tea.NewProgram(New(ctx, s), tea.WithContext(ctx)).Run()
	if err != nil {
		return nil, err
	}
	return final.(App).connectHost, nil
}

func (a App) Init() tea.Cmd {
	return tea.Batch(tea.RequestBackgroundColor, a.load(-1))
}

func (a App) load(selectID int64) tea.Cmd {
	return func() tea.Msg {
		hs, err := a.store.List(a.ctx, "")
		if err != nil {
			return statusMsg{text: err.Error(), isErr: true}
		}
		return hostsLoadedMsg{hosts: hs, selectID: selectID}
	}
}

func (a *App) setStatus(text string, isErr bool) tea.Cmd {
	a.status, a.statusErr = text, isErr
	a.statusSeq++
	seq := a.statusSeq
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return clearStatusMsg{seq} })
}

func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		a.theme = theme.New(msg.IsDark())
		a.hosts.SetTheme(a.theme)
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.hosts.SetSize(msg.Width-2, msg.Height-2) // margin + status line
		if a.form != nil {
			a.form = a.form.WithWidth(a.formWidth()).WithHeight(a.formHeight())
		}
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" && a.form == nil {
			return a, tea.Quit
		}
	case hostsLoadedMsg:
		a.hosts.SetHosts(msg.hosts)
		if msg.selectID >= 0 {
			a.hosts.Select(msg.selectID)
		}
		return a, nil
	case statusMsg:
		return a, a.setStatus(msg.text, msg.isErr)
	case clearStatusMsg:
		if msg.seq == a.statusSeq {
			a.status = ""
		}
		return a, nil
	}

	if a.form != nil {
		return a.updateForm(msg)
	}
	if a.confirm != nil {
		return a.updateConfirm(msg)
	}

	switch msg := msg.(type) {
	case hosts.QuitMsg:
		return a, tea.Quit
	case hosts.ConnectMsg:
		h := msg.Host
		a.connectHost = &h
		return a, tea.Quit
	case hosts.ShellMsg:
		return a, a.shell(msg.Host)
	case shellDoneMsg:
		cmds := []tea.Cmd{a.load(msg.host.ID)}
		if msg.err != nil {
			cmds = append(cmds, a.setStatus("ssh: "+msg.err.Error(), true))
		}
		return a, tea.Batch(cmds...)
	case hosts.CopyMsg:
		return a, tea.Batch(tea.SetClipboard(msg.Host.Target()),
			a.setStatus("Copied "+msg.Host.Target(), false))
	case hosts.AddMsg:
		return a, a.openForm("Add host", store.Host{})
	case hosts.EditMsg:
		return a, a.openForm("Edit "+msg.Host.Name, msg.Host)
	case hosts.DeleteMsg:
		a.pendingRm = msg.Host
		title := fmt.Sprintf("Delete %s?", msg.Host.Name)
		body := msg.Host.Target()
		if theme.IsProduction(msg.Host.Environment) {
			body += "\n\n" + lipgloss.NewStyle().Foreground(a.theme.Danger).Bold(true).
				Render("This is a PRODUCTION host.")
		}
		a.confirm = newConfirm(a.theme, title, body, "Delete")
		return a, nil
	}

	var cmd tea.Cmd
	a.hosts, cmd = a.hosts.Update(msg)
	return a, cmd
}

// shell runs ssh in the foreground and returns to the TUI afterwards.
func (a App) shell(h store.Host) tea.Cmd {
	args, err := sshx.ConnectArgs(h)
	if err != nil {
		return a.setStatusCmd(err.Error(), true)
	}
	_ = a.store.MarkUsed(a.ctx, h.ID)
	c := exec.Command("ssh", args...)
	return tea.ExecProcess(c, func(err error) tea.Msg {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			err = nil // the remote shell's exit status isn't our error
		}
		return shellDoneMsg{host: h, err: err}
	})
}

func (a App) setStatusCmd(text string, isErr bool) tea.Cmd {
	return func() tea.Msg { return statusMsg{text, isErr} }
}

func (a App) formWidth() int  { return max(30, min(72, a.width-6)) }
func (a App) formHeight() int { return max(10, a.height-6) }

func (a *App) openForm(title string, h store.Host) tea.Cmd {
	a.editing = h
	a.fields = hostform.FromHost(h)
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("esc", "ctrl+c"), key.WithHelp("esc", "cancel"))
	f := hostform.New(title, a.fields, func(name string) bool {
		other, err := a.store.Get(a.ctx, name)
		return err == nil && other.ID != h.ID
	}).
		WithKeyMap(km).
		WithShowHelp(true).
		WithTheme(huh.ThemeFunc(huh.ThemeCharm)).
		WithWidth(a.formWidth()).
		WithHeight(a.formHeight())
	f.SubmitCmd = func() tea.Msg { return formDoneMsg{} }
	f.CancelCmd = func() tea.Msg { return formDoneMsg{} }
	a.form = f
	return f.Init()
}

func (a App) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(formDoneMsg); ok {
		f := a.form
		a.form = nil
		if f.State != huh.StateCompleted {
			return a, nil
		}
		h := a.fields.Apply(a.editing)
		var err error
		var verb string
		if h.ID == 0 {
			h, err = a.store.Create(a.ctx, h)
			verb = "Added"
		} else {
			h, err = a.store.Update(a.ctx, h)
			verb = "Saved"
		}
		if err != nil {
			return a, a.setStatus(err.Error(), true)
		}
		return a, tea.Batch(a.load(h.ID), a.setStatus(verb+" "+h.Name, false))
	}
	m, cmd := a.form.Update(msg)
	if f, ok := m.(*huh.Form); ok {
		a.form = f
	}
	return a, cmd
}

func (a App) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return a, nil
	}
	done, yes := a.confirm.update(k)
	if !done {
		return a, nil
	}
	a.confirm = nil
	if !yes {
		return a, nil
	}
	h := a.pendingRm
	if err := a.store.Delete(a.ctx, h.ID); err != nil {
		return a, a.setStatus(err.Error(), true)
	}
	return a, tea.Batch(a.load(-1), a.setStatus("Deleted "+h.Name, false))
}

func (a App) View() tea.View {
	base := lipgloss.NewStyle().Margin(0, 1).Render(a.hosts.View())
	status := ""
	if a.status != "" {
		st := a.theme.Status
		if a.statusErr {
			st = a.theme.Error
		}
		status = " " + st.Render(a.status)
	}
	base = lipgloss.JoinVertical(lipgloss.Left, base, status)

	var overlay string
	switch {
	case a.form != nil:
		overlay = a.theme.Border.BorderForeground(a.theme.Accent).Padding(0, 1).Render(a.form.View())
	case a.confirm != nil:
		overlay = a.confirm.view()
	}
	content := base
	if overlay != "" && a.width > 0 {
		x := max(0, (a.width-lipgloss.Width(overlay))/2)
		y := max(0, (a.height-lipgloss.Height(overlay))/2)
		canvas := lipgloss.NewCanvas(a.width, a.height)
		canvas.Compose(lipgloss.NewCompositor(
			lipgloss.NewLayer(base),
			lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
		))
		content = canvas.Render()
	}

	v := tea.NewView(content)
	v.AltScreen = true
	v.WindowTitle = "rootnet"
	return v
}
