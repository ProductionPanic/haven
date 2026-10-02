// Package files is the two-pane file manager: local on the left, the
// remote host on the right, transfers at the bottom.
package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/progress"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ProductionPanic/rootnet-cli/internal/store"
	"github.com/ProductionPanic/rootnet-cli/internal/transfer"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/theme"
	"github.com/ProductionPanic/rootnet-cli/internal/vfs"
)

// Dialer opens the remote file system for a host.
type Dialer func(ctx context.Context, h store.Host) (*vfs.RemoteFS, error)

// Config describes how to start the file manager.
type Config struct {
	Host      store.Host
	Dial      Dialer
	LocalFS   vfs.FS // defaults to vfs.LocalFS{}
	LocalDir  string // start directory on the left
	RemoteDir string // start directory on the right ("" → host remote path or home)
	Workers   int
}

// CloseMsg is emitted when the user leaves the file manager. LocalDir and
// RemoteDir are where the panes ended up, for remembering.
type CloseMsg struct {
	Host                store.Host
	LocalDir, RemoteDir string
}

type (
	connectedMsg struct {
		fs  *vfs.RemoteFS
		dir string
		err error
	}
	dirMsg struct {
		side       int
		dir        string
		entries    []vfs.Entry
		selectName string
		err        error
	}
	opDoneMsg struct {
		what  string
		err   error
		sides []int
		sel   string
	}
	engineMsg   struct{ ev transfer.Event }
	clearStatus struct{ seq int }
)

const (
	left  = 0
	right = 1
)

type promptKind int

const (
	promptNone promptKind = iota
	promptMkdir
	promptRename
	promptFilter
)

// Model is the file manager.
type Model struct {
	Keys KeyMap
	cfg  Config
	ctx  context.Context
	stop context.CancelFunc

	theme theme.Theme
	help  help.Model
	bar   progress.Model

	panes  [2]*pane
	active int
	remote *vfs.RemoteFS

	engine   *transfer.Engine
	batches  map[int]transfer.Progress
	conflict *transfer.Conflict
	forAll   bool

	prompt     promptKind
	input      textinput.Model
	renameFrom string

	confirmDelete []string
	confirmSide   int

	status    string
	statusErr bool
	statusSeq int

	width, height int
	now           func() time.Time
}

// New creates the file manager. Call Init to connect.
func New(ctx context.Context, t theme.Theme, cfg Config) Model {
	if cfg.LocalFS == nil {
		cfg.LocalFS = vfs.LocalFS{}
	}
	if cfg.LocalDir == "" {
		cfg.LocalDir, _ = os.Getwd()
	}
	ctx, cancel := context.WithCancel(ctx)
	m := Model{
		Keys:    DefaultKeyMap(),
		cfg:     cfg,
		ctx:     ctx,
		stop:    cancel,
		help:    help.New(),
		bar:     progress.New(progress.WithDefaultBlend(), progress.WithoutPercentage()),
		engine:  transfer.New(max(1, cfg.Workers)),
		batches: map[int]transfer.Progress{},
		input:   textinput.New(),
		now:     time.Now,
	}
	m.panes[left] = newPane(cfg.LocalFS.Name())
	m.panes[left].fs = cfg.LocalFS
	m.panes[left].loading = true
	m.panes[right] = newPane(cfg.Host.Name)
	m.panes[right].loading = true
	m.SetTheme(t)
	return m
}

// SetTheme applies a theme.
func (m *Model) SetTheme(t theme.Theme) {
	m.theme = t
	m.help.Styles = help.DefaultStyles(t.IsDark)
	m.input.SetStyles(textinput.DefaultStyles(t.IsDark))
}

// SetSize sets the available space.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.help.SetWidth(w)
	m.layout()
}

func (m *Model) layout() {
	used := 2 // help + status
	if m.help.ShowAll {
		used += 5
	}
	used += m.transferLines()
	ph := max(5, m.height-used)
	lw := m.width / 2
	m.panes[left].width, m.panes[left].height = lw, ph
	m.panes[right].width, m.panes[right].height = m.width-lw, ph
	for _, p := range m.panes {
		p.clamp()
	}
	m.bar.SetWidth(max(10, min(30, m.width/4)))
}

func (m Model) transferLines() int { return min(3, len(m.batches)) }

// Init connects to the host and lists the local directory.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.connect(), m.list(left, m.cfg.LocalDir, ""), m.waitEngine())
}

func (m Model) connect() tea.Cmd {
	cfg, ctx := m.cfg, m.ctx
	return func() tea.Msg {
		r, err := cfg.Dial(ctx, cfg.Host)
		if err != nil {
			return connectedMsg{err: err}
		}
		start := cfg.RemoteDir
		if start == "" {
			start = cfg.Host.RemotePath
		}
		dir, err := r.Resolve(start)
		if err == nil {
			if e, serr := r.Stat(dir); serr != nil || !e.IsDir {
				dir, err = r.Resolve("") // fall back to home
			}
		}
		return connectedMsg{fs: r, dir: dir, err: err}
	}
}

func (m Model) list(side int, dir, selectName string) tea.Cmd {
	fsys := m.panes[side].fs
	if fsys == nil {
		return nil
	}
	return func() tea.Msg {
		entries, err := fsys.ReadDir(dir)
		return dirMsg{side: side, dir: dir, entries: entries, selectName: selectName, err: err}
	}
}

func (m Model) waitEngine() tea.Cmd {
	ch := m.engine.Events()
	return func() tea.Msg { return engineMsg{<-ch} }
}

func (m *Model) setStatus(text string, isErr bool) tea.Cmd {
	m.status, m.statusErr = text, isErr
	m.statusSeq++
	seq := m.statusSeq
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return clearStatus{seq} })
}

// Host returns the host being browsed.
func (m Model) Host() store.Host { return m.cfg.Host }

// Close cancels transfers and closes the connection.
func (m Model) Close() {
	m.stop()
	m.engine.CancelAll()
	if m.remote != nil {
		m.remote.Close()
	}
}

func (m Model) closeMsg() tea.Cmd {
	msg := CloseMsg{Host: m.cfg.Host, LocalDir: m.panes[left].cwd, RemoteDir: m.panes[right].cwd}
	m.Close()
	return func() tea.Msg { return msg }
}

// Update handles messages.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	switch msg := msg.(type) {
	case connectedMsg:
		p := m.panes[right]
		if msg.err != nil {
			p.loading, p.err = false, msg.err.Error()
			return m, m.setStatus("Could not connect: "+msg.err.Error(), true)
		}
		m.remote = msg.fs
		p.fs = msg.fs
		return m, m.list(right, msg.dir, "")

	case dirMsg:
		p := m.panes[msg.side]
		if msg.err != nil {
			p.loading = false
			if p.cwd == "" {
				p.err = msg.err.Error()
				return m, nil
			}
			return m, m.setStatus(msg.err.Error(), true)
		}
		p.setEntries(msg.dir, msg.entries, msg.selectName)
		return m, nil

	case opDoneMsg:
		var cmds []tea.Cmd
		for _, s := range msg.sides {
			cmds = append(cmds, m.list(s, m.panes[s].cwd, msg.sel))
		}
		if msg.err != nil {
			cmds = append(cmds, m.setStatus(msg.err.Error(), true))
		} else if msg.what != "" {
			cmds = append(cmds, m.setStatus(msg.what, false))
		}
		return m, tea.Batch(cmds...)

	case engineMsg:
		return m.handleEngine(msg.ev)

	case clearStatus:
		if msg.seq == m.statusSeq {
			m.status = ""
		}
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	if m.prompt != promptNone {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) handleEngine(ev transfer.Event) (Model, tea.Cmd) {
	cmds := []tea.Cmd{m.waitEngine()}
	switch ev := ev.(type) {
	case transfer.Conflict:
		c := ev
		m.conflict = &c
		m.forAll = false
	case transfer.Progress:
		if !ev.Finished {
			m.batches[ev.Batch] = ev
			break
		}
		delete(m.batches, ev.Batch)
		// Refresh whichever pane shows the destination.
		cmds = append(cmds, m.list(left, m.panes[left].cwd, ""), m.list(right, m.panes[right].cwd, ""))
		switch {
		case ev.Cancelled:
			cmds = append(cmds, m.setStatus("Transfer cancelled", true))
		case ev.Err != nil:
			cmds = append(cmds, m.setStatus(ev.Err.Error(), true))
		default:
			copied := ev.Settled - ev.Skipped - ev.Failed
			s := fmt.Sprintf("Copied %d file(s), %s", copied, transfer.FormatBytes(ev.Done))
			if ev.Skipped > 0 {
				s += fmt.Sprintf(", skipped %d", ev.Skipped)
			}
			cmds = append(cmds, m.setStatus(s, false))
		}
	}
	m.layout()
	return m, tea.Batch(cmds...)
}

func (m Model) handleKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch {
	case m.conflict != nil:
		return m.handleConflictKey(k)
	case m.confirmDelete != nil:
		return m.handleDeleteKey(k)
	case m.prompt != promptNone:
		return m.handlePromptKey(k)
	}

	p := m.panes[m.active]
	switch {
	case key.Matches(k, m.Keys.Quit):
		if p.filter != "" && k.String() == "esc" {
			p.filter = ""
			p.refilter()
			return m, nil
		}
		return m, m.closeMsg()
	case key.Matches(k, m.Keys.Switch):
		m.active = 1 - m.active
	case key.Matches(k, m.Keys.Up):
		p.move(-1)
	case key.Matches(k, m.Keys.Down):
		p.move(1)
	case key.Matches(k, m.Keys.PageUp):
		p.move(-p.listHeight())
	case key.Matches(k, m.Keys.PageDown):
		p.move(p.listHeight())
	case key.Matches(k, m.Keys.Home):
		p.cursor = 0
		p.clamp()
	case key.Matches(k, m.Keys.End):
		p.cursor = len(p.visible) - 1
		p.clamp()
	case key.Matches(k, m.Keys.Open):
		e, ok := p.selected()
		if !ok || p.fs == nil {
			break
		}
		if e.Name == parentName {
			return m, m.up(p)
		}
		if e.IsDir {
			p.loading = true
			return m, m.list(m.active, p.fs.Join(p.cwd, e.Name), "")
		}
	case key.Matches(k, m.Keys.Parent):
		return m, m.up(p)
	case key.Matches(k, m.Keys.GoHome):
		if p.fs != nil {
			if home, err := p.fs.Home(); err == nil {
				return m, m.list(m.active, home, "")
			}
		}
	case key.Matches(k, m.Keys.Refresh):
		return m, m.list(m.active, p.cwd, "")
	case key.Matches(k, m.Keys.Mark):
		p.toggleMark()
		p.move(1)
	case key.Matches(k, m.Keys.MarkAll):
		p.markAll()
	case key.Matches(k, m.Keys.Hidden):
		p.showHidden = !p.showHidden
		p.refilter()
	case key.Matches(k, m.Keys.Sort):
		p.sortBy = (p.sortBy + 1) % 3
		p.refilter()
		return m, m.setStatus("Sorted by "+p.sortBy.String(), false)
	case key.Matches(k, m.Keys.Filter):
		return m, m.openPrompt(promptFilter, "/ ", p.filter, "")
	case key.Matches(k, m.Keys.Copy):
		return m, m.copy()
	case key.Matches(k, m.Keys.Mkdir):
		if p.fs != nil && p.cwd != "" {
			return m, m.openPrompt(promptMkdir, "New directory: ", "", "")
		}
	case key.Matches(k, m.Keys.Rename):
		if e, ok := p.selected(); ok && e.Name != parentName {
			return m, m.openPrompt(promptRename, "Rename to: ", e.Name, e.Name)
		}
	case key.Matches(k, m.Keys.Delete):
		if t := p.targets(); len(t) > 0 {
			m.confirmDelete = p.paths(t)
			m.confirmSide = m.active
		}
	case key.Matches(k, m.Keys.CancelTransfers):
		if len(m.batches) > 0 {
			m.engine.CancelAll()
		}
	case key.Matches(k, m.Keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.layout()
	}
	return m, nil
}

func (m Model) up(p *pane) tea.Cmd {
	if p.fs == nil || p.cwd == "" {
		return nil
	}
	parent := p.fs.Dir(p.cwd)
	if parent == p.cwd {
		return nil
	}
	side := left
	if p == m.panes[right] {
		side = right
	}
	return m.list(side, parent, p.fs.Base(p.cwd))
}

func (m *Model) openPrompt(kind promptKind, prompt, value, from string) tea.Cmd {
	m.prompt = kind
	m.renameFrom = from
	m.input.Prompt = prompt
	m.input.SetValue(value)
	m.input.CursorEnd()
	m.input.SetWidth(max(20, m.width/2))
	return m.input.Focus()
}

func (m Model) handlePromptKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	p := m.panes[m.active]
	switch k.String() {
	case "esc":
		if m.prompt == promptFilter {
			p.filter = ""
			p.refilter()
		}
		m.prompt = promptNone
		m.input.Blur()
		return m, nil
	case "enter":
		kind, value := m.prompt, strings.TrimSpace(m.input.Value())
		m.prompt = promptNone
		m.input.Blur()
		switch kind {
		case promptFilter:
			return m, nil
		case promptMkdir:
			if value == "" {
				return m, nil
			}
			return m, m.op(fmt.Sprintf("Created %s", value), []int{m.active}, value, func() error {
				return p.fs.Mkdir(p.fs.Join(p.cwd, value))
			})
		case promptRename:
			if value == "" || value == m.renameFrom {
				return m, nil
			}
			if strings.ContainsAny(value, "/") {
				return m, m.setStatus("Name may not contain /", true)
			}
			from, to := p.fs.Join(p.cwd, m.renameFrom), p.fs.Join(p.cwd, value)
			return m, m.op(fmt.Sprintf("Renamed to %s", value), []int{m.active}, value, func() error {
				if _, err := p.fs.Stat(to); err == nil {
					return fmt.Errorf("%s already exists", value)
				}
				return p.fs.Rename(from, to)
			})
		}
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	if m.prompt == promptFilter {
		p.filter = m.input.Value()
		p.refilter()
		p.cursor = 0
		p.clamp()
	}
	return m, cmd
}

func (m Model) op(what string, sides []int, sel string, f func() error) tea.Cmd {
	return func() tea.Msg {
		err := f()
		if err != nil {
			what = ""
		}
		return opDoneMsg{what: what, err: err, sides: sides, sel: sel}
	}
}

func (m Model) handleDeleteKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "y", "Y":
		paths := m.confirmDelete
		p := m.panes[m.confirmSide]
		m.confirmDelete = nil
		p.marked = map[string]bool{}
		return m, m.op(fmt.Sprintf("Deleted %d item(s)", len(paths)), []int{m.confirmSide}, "", func() error {
			var errs []error
			for _, path := range paths {
				if err := p.fs.Remove(path); err != nil {
					errs = append(errs, err)
				}
			}
			return errors.Join(errs...)
		})
	case "n", "N", "esc", "q", "enter":
		m.confirmDelete = nil
	}
	return m, nil
}

func (m Model) handleConflictKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	var pol transfer.Policy
	switch k.String() {
	case "a":
		m.forAll = !m.forAll
		return m, nil
	case "o":
		pol = transfer.Overwrite
	case "s", "esc":
		pol = transfer.Skip
	case "n":
		pol = transfer.OverwriteIfNewer
	case "r":
		pol = transfer.RenameNew
	case "x":
		m.conflict.Reply <- transfer.Decision{Policy: transfer.Skip}
		m.engine.Cancel(m.conflict.Batch)
		m.conflict = nil
		return m, nil
	default:
		return m, nil
	}
	m.conflict.Reply <- transfer.Decision{Policy: pol, ForAll: m.forAll}
	m.conflict = nil
	return m, nil
}

// copy starts a transfer of the active pane's targets into the other pane.
func (m Model) copy() tea.Cmd {
	src, dst := m.panes[m.active], m.panes[1-m.active]
	if src.fs == nil || dst.fs == nil || dst.cwd == "" {
		return m.setStatusCmd("Not connected yet", true)
	}
	targets := src.targets()
	if len(targets) == 0 {
		return nil
	}
	m.engine.Start(m.ctx, transfer.Request{
		Src: src.fs, Sources: src.paths(targets),
		Dst: dst.fs, DstDir: dst.cwd,
		Policy: transfer.Ask,
	})
	src.marked = map[string]bool{}
	return nil
}

func (m Model) setStatusCmd(text string, isErr bool) tea.Cmd {
	return func() tea.Msg { return opDoneMsg{err: errorf(isErr, text), what: text} }
}

func errorf(isErr bool, s string) error {
	if !isErr {
		return nil
	}
	return errors.New(s)
}

// View renders the file manager.
func (m Model) View() string {
	t := m.theme
	now := m.now()
	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		m.panes[left].view(t, m.active == left, now),
		m.panes[right].view(t, m.active == right, now))

	parts := []string{panes}
	if tl := m.transferView(); tl != "" {
		parts = append(parts, tl)
	}
	status := ""
	switch {
	case m.prompt != promptNone:
		status = m.input.View()
	case m.status != "":
		st := t.Status
		if m.statusErr {
			st = t.Error
		}
		status = st.Render(ansi.Truncate(m.status, m.width, "…"))
	case m.panes[right].fs == nil && m.panes[right].err == "":
		status = t.Faint.Render("Connecting to " + m.cfg.Host.Name + "…")
	}
	parts = append(parts, status, m.help.View(m.Keys))
	base := lipgloss.JoinVertical(lipgloss.Left, parts...)

	var overlay string
	switch {
	case m.conflict != nil:
		overlay = m.conflictView()
	case m.confirmDelete != nil:
		overlay = m.deleteView()
	}
	if overlay == "" || m.width == 0 {
		return base
	}
	x := max(0, (m.width-lipgloss.Width(overlay))/2)
	y := max(0, (m.height-lipgloss.Height(overlay))/2)
	canvas := lipgloss.NewCanvas(m.width, max(m.height, lipgloss.Height(base)))
	canvas.Compose(lipgloss.NewCompositor(
		lipgloss.NewLayer(base),
		lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
	))
	return canvas.Render()
}

func (m Model) transferView() string {
	if len(m.batches) == 0 {
		return ""
	}
	ids := make([]int, 0, len(m.batches))
	for id := range m.batches {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	var lines []string
	for i, id := range ids {
		if i == 3 {
			break
		}
		p := m.batches[id]
		info := fmt.Sprintf(" %3.0f%%  %s/%s", p.Fraction()*100, transfer.FormatBytes(p.Done), transfer.FormatBytes(p.Total))
		if p.Rate > 0 {
			info += "  " + transfer.FormatRate(p.Rate)
		}
		if eta := p.ETA(); eta != "" {
			info += "  ETA " + eta
		}
		label := p.Label
		if p.Current != "" && p.Files > 1 {
			label += fmt.Sprintf(" (%d/%d: %s)", p.Settled+1, p.Files, p.Current)
		}
		labelW := max(10, m.width-m.bar.Width()-lipgloss.Width(info)-3)
		label = ansi.Truncate(label, labelW, "…")
		label += strings.Repeat(" ", max(0, labelW-lipgloss.Width(label)))
		lines = append(lines, " "+label+" "+m.bar.ViewAs(p.Fraction())+m.theme.Faint.Render(info))
	}
	return strings.Join(lines, "\n")
}

func (m Model) conflictView() string {
	t := m.theme
	c := m.conflict
	src := c.File
	fmtInfo := func(size int64, mod time.Time) string {
		return fmt.Sprintf("%s · %s", transfer.FormatBytes(size), mod.Local().Format("2 Jan 2006 15:04"))
	}
	newer := ""
	switch {
	case src.ModTime.After(c.Existing.ModTime):
		newer = t.Status.Render("  (newer)")
	case src.ModTime.Before(c.Existing.ModTime):
		newer = t.Faint.Render("  (older)")
	}
	check := "[ ]"
	if m.forAll {
		check = "[x]"
	}
	lines := []string{
		lipgloss.NewStyle().Bold(true).Render("File already exists"),
		"",
		ansi.Truncate(src.DstPath, 70, "…"),
		t.Label.Render("Existing") + fmtInfo(c.Existing.Size, c.Existing.ModTime),
		t.Label.Render("New") + fmtInfo(src.Size, src.ModTime) + newer,
	}
	if m.remote != nil && m.panes[right].fs != nil && theme.IsProduction(m.cfg.Host.Environment) {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(t.Danger).Bold(true).Render("This is a PRODUCTION host."))
	}
	lines = append(lines, "",
		"o overwrite   s skip   n overwrite if newer   r keep both",
		check+" a apply to all remaining   x cancel transfer")
	return t.Border.BorderForeground(t.Warning).Padding(1, 2).Render(strings.Join(lines, "\n"))
}

func (m Model) deleteView() string {
	t := m.theme
	p := m.panes[m.confirmSide]
	n := len(m.confirmDelete)
	title := fmt.Sprintf("Delete %d items?", n)
	if n == 1 {
		title = fmt.Sprintf("Delete %s?", p.fs.Base(m.confirmDelete[0]))
	}
	lines := []string{lipgloss.NewStyle().Bold(true).Render(title), ""}
	for i, path := range m.confirmDelete {
		if i == 6 {
			lines = append(lines, t.Faint.Render(fmt.Sprintf("… and %d more", n-6)))
			break
		}
		lines = append(lines, ansi.Truncate(path, 70, "…"))
	}
	where := "on this computer"
	if p.fs.Remote() {
		where = "on " + m.cfg.Host.Name
	}
	lines = append(lines, "", t.Faint.Render("Directories are deleted recursively "+where+"."))
	if p.fs.Remote() && theme.IsProduction(m.cfg.Host.Environment) {
		lines = append(lines, lipgloss.NewStyle().Foreground(t.Danger).Bold(true).Render("This is a PRODUCTION host."))
	}
	lines = append(lines, "", "y delete   n cancel")
	return t.Border.BorderForeground(t.Danger).Padding(1, 2).Render(strings.Join(lines, "\n"))
}
