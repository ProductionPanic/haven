// Package hosts is the host manager screen: a filterable host list with a
// detail panel.
package hosts

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ProductionPanic/rootnet-cli/internal/match"
	"github.com/ProductionPanic/rootnet-cli/internal/sshx"
	"github.com/ProductionPanic/rootnet-cli/internal/store"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/theme"
)

// Messages emitted for the root app to act on.
type (
	ConnectMsg struct{ Host store.Host } // ssh and exit
	ShellMsg   struct{ Host store.Host } // ssh and come back
	AddMsg     struct{}
	EditMsg    struct{ Host store.Host }
	DeleteMsg  struct{ Host store.Host }
	CopyMsg    struct{ Host store.Host }
	QuitMsg    struct{}
)

// row is a line in the list: either a group header or a host.
type row struct {
	header string
	count  int
	host   *store.Host
}

// Model is the hosts screen.
type Model struct {
	Keys KeyMap

	theme  theme.Theme
	help   help.Model
	filter textinput.Model

	all     []store.Host
	tags    []string
	tag     string
	grouped bool

	rows   []row
	cursor int // index into rows; always a host row when any exist
	offset int // first visible row

	width, height int
	now           func() time.Time
}

// New returns an empty hosts screen.
func New(t theme.Theme) Model {
	ti := textinput.New()
	ti.Prompt = "/ "
	ti.Placeholder = "filter hosts"
	m := Model{
		Keys:   DefaultKeyMap(),
		help:   help.New(),
		filter: ti,
		now:    time.Now,
	}
	m.SetTheme(t)
	return m
}

// SetTheme applies a theme.
func (m *Model) SetTheme(t theme.Theme) {
	m.theme = t
	m.help.Styles = help.DefaultStyles(t.IsDark)
	s := textinput.DefaultStyles(t.IsDark)
	s.Focused.Prompt = s.Focused.Prompt.Foreground(t.Accent)
	m.filter.SetStyles(s)
}

// SetSize sets the space available to the screen.
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.help.SetWidth(w)
	m.filter.SetWidth(max(10, m.listWidth()-4))
	m.clampScroll()
}

// SetHosts replaces the host list, keeping the selection where possible.
func (m *Model) SetHosts(hosts []store.Host) {
	m.all = hosts
	seen := map[string]bool{}
	m.tags = m.tags[:0]
	for _, h := range hosts {
		for _, t := range h.Tags {
			if !seen[t] {
				seen[t] = true
				m.tags = append(m.tags, t)
			}
		}
	}
	sort.Strings(m.tags)
	if m.tag != "" && !seen[m.tag] {
		m.tag = ""
	}
	m.rebuild()
}

// Select moves the cursor to the host with the given ID, if visible.
func (m *Model) Select(id int64) {
	for i, r := range m.rows {
		if r.host != nil && r.host.ID == id {
			m.cursor = i
			m.clampScroll()
			return
		}
	}
}

// Selected returns the host under the cursor.
func (m Model) Selected() (store.Host, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) || m.rows[m.cursor].host == nil {
		return store.Host{}, false
	}
	return *m.rows[m.cursor].host, true
}

// Filtering reports whether the filter input has focus.
func (m Model) Filtering() bool { return m.filter.Focused() }

func (m *Model) rebuild() {
	var prev int64 = -1
	if h, ok := m.Selected(); ok {
		prev = h.ID
	}

	pool := m.all
	if m.tag != "" {
		pool = nil
		for _, h := range m.all {
			for _, t := range h.Tags {
				if t == m.tag {
					pool = append(pool, h)
					break
				}
			}
		}
	}
	found := match.Find(pool, m.filter.Value(), m.now())

	m.rows = m.rows[:0]
	if m.grouped {
		// Stable sort keeps frecency order within each server.
		sort.SliceStable(found, func(i, j int) bool {
			return strings.ToLower(found[i].Hostname) < strings.ToLower(found[j].Hostname)
		})
		for i := 0; i < len(found); {
			j := i
			for j < len(found) && strings.EqualFold(found[j].Hostname, found[i].Hostname) {
				j++
			}
			m.rows = append(m.rows, row{header: found[i].Hostname, count: j - i})
			for k := i; k < j; k++ {
				m.rows = append(m.rows, row{host: &found[k]})
			}
			i = j
		}
	} else {
		for i := range found {
			m.rows = append(m.rows, row{host: &found[i]})
		}
	}

	m.cursor = 0
	if prev >= 0 {
		m.Select(prev)
	}
	if m.cursor < len(m.rows) && m.rows[m.cursor].host == nil {
		m.move(1)
	}
	m.clampScroll()
}

// move shifts the cursor by delta host rows, skipping headers.
func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	for n := abs(delta); n > 0; n-- {
		i := m.cursor + step
		for i >= 0 && i < len(m.rows) && m.rows[i].host == nil {
			i += step
		}
		if i < 0 || i >= len(m.rows) {
			break
		}
		m.cursor = i
	}
	// The cursor may still be on a header after a rebuild.
	if m.rows[m.cursor].host == nil {
		for i := m.cursor; i < len(m.rows); i++ {
			if m.rows[i].host != nil {
				m.cursor = i
				break
			}
		}
	}
	m.clampScroll()
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m Model) listHeight() int {
	// title, filter line, blank, help (+ full help lines)
	h := m.height - 4
	if m.help.ShowAll {
		h -= 5
	}
	return max(1, h)
}

func (m Model) showDetail() bool { return m.width >= 90 }

func (m Model) listWidth() int {
	if !m.showDetail() {
		return m.width
	}
	return m.width * 45 / 100
}

func (m *Model) clampScroll() {
	h := m.listHeight()
	if m.cursor < m.offset {
		m.offset = m.cursor
		// keep the group header visible
		if m.offset > 0 && m.rows[m.offset-1].host == nil {
			m.offset--
		}
	}
	if m.cursor >= m.offset+h {
		m.offset = m.cursor - h + 1
	}
	m.offset = max(0, min(m.offset, len(m.rows)-h))
}

// Update handles input.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		if m.filter.Focused() {
			var cmd tea.Cmd
			m.filter, cmd = m.filter.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	selected := func(f func(store.Host) tea.Msg) tea.Cmd {
		h, ok := m.Selected()
		if !ok {
			return nil
		}
		return func() tea.Msg { return f(h) }
	}

	// Navigation and connecting work in both modes.
	switch {
	case key.Matches(k, m.Keys.Up) && (!m.filter.Focused() || k.String() != "k"):
		m.move(-1)
		return m, nil
	case key.Matches(k, m.Keys.Down) && (!m.filter.Focused() || k.String() != "j"):
		m.move(1)
		return m, nil
	case key.Matches(k, m.Keys.PageUp):
		m.move(-m.listHeight())
		return m, nil
	case key.Matches(k, m.Keys.PageDown):
		m.move(m.listHeight())
		return m, nil
	case key.Matches(k, m.Keys.Connect):
		return m, selected(func(h store.Host) tea.Msg { return ConnectMsg{h} })
	}

	if m.filter.Focused() {
		if key.Matches(k, m.Keys.ClearFilter) {
			m.filter.Blur()
			return m, nil
		}
		before := m.filter.Value()
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if m.filter.Value() != before {
			m.rebuild()
			if len(m.rows) > 0 {
				m.cursor = 0
				m.move(0)
				m.offset = 0
			}
		}
		return m, cmd
	}

	switch {
	case key.Matches(k, m.Keys.Home):
		m.cursor = 0
		m.move(0)
	case key.Matches(k, m.Keys.End):
		m.cursor = len(m.rows) - 1
		m.move(0)
	case key.Matches(k, m.Keys.Filter):
		return m, m.filter.Focus()
	case key.Matches(k, m.Keys.ClearFilter):
		if m.filter.Value() != "" || m.tag != "" {
			m.filter.SetValue("")
			m.tag = ""
			m.rebuild()
		}
	case key.Matches(k, m.Keys.Tag):
		m.tag = nextTag(m.tags, m.tag)
		m.rebuild()
	case key.Matches(k, m.Keys.Group):
		m.grouped = !m.grouped
		m.rebuild()
	case key.Matches(k, m.Keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		m.clampScroll()
	case key.Matches(k, m.Keys.Shell):
		return m, selected(func(h store.Host) tea.Msg { return ShellMsg{h} })
	case key.Matches(k, m.Keys.Add):
		return m, func() tea.Msg { return AddMsg{} }
	case key.Matches(k, m.Keys.Edit):
		return m, selected(func(h store.Host) tea.Msg { return EditMsg{h} })
	case key.Matches(k, m.Keys.Delete):
		return m, selected(func(h store.Host) tea.Msg { return DeleteMsg{h} })
	case key.Matches(k, m.Keys.Copy):
		return m, selected(func(h store.Host) tea.Msg { return CopyMsg{h} })
	case key.Matches(k, m.Keys.Quit):
		return m, func() tea.Msg { return QuitMsg{} }
	}
	return m, nil
}

func nextTag(tags []string, cur string) string {
	if len(tags) == 0 {
		return ""
	}
	if cur == "" {
		return tags[0]
	}
	for i, t := range tags {
		if t == cur && i+1 < len(tags) {
			return tags[i+1]
		}
	}
	return ""
}

// View renders the screen.
func (m Model) View() string {
	t := m.theme
	title := t.Title.Render("rootnet")
	count := 0
	for _, r := range m.rows {
		if r.host != nil {
			count++
		}
	}
	info := fmt.Sprintf("%d of %d hosts", count, len(m.all))
	if m.tag != "" {
		info += " · " + t.Tag.Render("#"+m.tag)
	}
	if m.grouped {
		info += " · grouped by server"
	}
	header := title + "  " + t.Faint.Render(info)

	filterLine := m.filter.View()
	if !m.filter.Focused() && m.filter.Value() == "" {
		filterLine = t.Faint.Render("/ to filter · t tag · g group")
	}

	list := m.listView()
	body := list
	if m.showDetail() {
		detailW := m.width - m.listWidth() - 1
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, " ", m.detailView(detailW, m.listHeight()))
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, filterLine, "", body, m.help.View(m.Keys))
}

func (m Model) listView() string {
	t := m.theme
	w, h := m.listWidth(), m.listHeight()
	if len(m.rows) == 0 {
		msg := "No hosts yet. Press a to add one."
		if len(m.all) > 0 {
			msg = "No hosts match."
		}
		return lipgloss.NewStyle().Width(w).Height(h).Render(t.Faint.Render("  " + msg))
	}

	nameW := 0
	for _, r := range m.rows {
		if r.host != nil {
			nameW = max(nameW, lipgloss.Width(r.host.Name))
		}
	}
	nameW = min(nameW, w/2)

	var lines []string
	end := min(len(m.rows), m.offset+h)
	for i := m.offset; i < end; i++ {
		r := m.rows[i]
		if r.host == nil {
			lines = append(lines, ansi.Truncate(t.Faint.Bold(true).Render(fmt.Sprintf("  %s (%d)", r.header, r.count)), w, "…"))
			continue
		}
		hst := r.host
		cursor := "  "
		nameStyle := lipgloss.NewStyle()
		if i == m.cursor {
			cursor = t.Selected.Render("▌ ")
			nameStyle = t.Selected
		}
		dot := " "
		if c := t.EnvColor(hst.Environment); c != nil {
			dot = lipgloss.NewStyle().Foreground(c).Render("●")
		}
		name := ansi.Truncate(hst.Name, nameW, "…")
		pad := strings.Repeat(" ", max(0, nameW-lipgloss.Width(name)))
		target := ""
		if !m.grouped {
			target = "  " + t.Faint.Render(hst.Target())
		}
		line := cursor + dot + " " + nameStyle.Render(name) + pad + target
		lines = append(lines, ansi.Truncate(line, w, "…"))
	}
	return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Render(strings.Join(lines, "\n"))
}

func (m Model) detailView(w, h int) string {
	t := m.theme
	box := t.Border.Width(w).Height(h).MaxHeight(h).Padding(0, 1)
	hst, ok := m.Selected()
	if !ok {
		return box.Render("")
	}
	inner := w - box.GetHorizontalFrameSize()

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Bold(true).Render(hst.Name))
	if badge := t.Badge(hst.Environment); badge != "" {
		b.WriteString("  " + badge)
	}
	b.WriteString("\n\n")

	field := func(label, value string) {
		if value == "" {
			return
		}
		b.WriteString(t.Label.Render(label) + ansi.Truncate(value, inner-t.Label.GetWidth(), "…") + "\n")
	}
	field("User", hst.User)
	field("Host", hst.Hostname)
	if hst.Port != 22 && hst.Port != 0 {
		field("Port", strconv.Itoa(hst.Port))
	}
	field("Path", hst.RemotePath)
	field("Key", hst.IdentityFile)
	field("Jump", hst.JumpHost)
	field("Extra args", hst.ExtraArgs)
	if len(hst.Tags) > 0 {
		field("Tags", t.Tag.Render("#"+strings.Join(hst.Tags, " #")))
	}
	last := "never"
	if hst.LastUsedAt != nil {
		last = fmt.Sprintf("%s (%d×)", humanize(m.now().Sub(*hst.LastUsedAt)), hst.UseCount)
	}
	field("Last used", last)

	if args, err := sshx.ConnectArgs(hst); err == nil {
		b.WriteString("\n" + t.Faint.Render(ansi.Wrap("$ ssh "+strings.Join(quoteAll(args), " "), inner, "")) + "\n")
	}
	if hst.Notes != "" {
		b.WriteString("\n" + lipgloss.Wrap(hst.Notes, inner, "") + "\n")
	}
	return box.Render(strings.TrimRight(b.String(), "\n"))
}

func quoteAll(args []string) []string {
	out := make([]string, len(args))
	for i, a := range args {
		out[i] = sshx.Quote(a)
	}
	return out
}

func humanize(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
