package files

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ProductionPanic/rootnet-cli/internal/transfer"
	"github.com/ProductionPanic/rootnet-cli/internal/tui/theme"
	"github.com/ProductionPanic/rootnet-cli/internal/vfs"
)

// pane is one side of the file manager.
type pane struct {
	fs    vfs.FS // nil while connecting
	label string
	cwd   string

	all     []vfs.Entry
	visible []vfs.Entry // after hidden/filter, with ".." first
	cursor  int
	offset  int
	marked  map[string]bool

	showHidden bool
	sortBy     vfs.SortBy
	filter     string

	loading bool
	err     string

	width, height int
}

func newPane(label string) *pane {
	return &pane{label: label, marked: map[string]bool{}}
}

const parentName = ".."

// setEntries installs a fresh listing for dir and puts the cursor on
// selectName when given.
func (p *pane) setEntries(dir string, entries []vfs.Entry, selectName string) {
	if dir != p.cwd {
		p.marked = map[string]bool{}
		p.filter = ""
		p.offset = 0
	}
	prev := ""
	if e, ok := p.selected(); ok {
		prev = e.Name
	}
	p.cwd, p.all, p.loading, p.err = dir, entries, false, ""
	p.refilter()
	switch {
	case selectName != "":
		p.selectName(selectName)
	case prev != "":
		p.selectName(prev)
	default:
		p.cursor = 0
	}
	// Drop marks for entries that vanished.
	names := map[string]bool{}
	for _, e := range p.all {
		names[e.Name] = true
	}
	for n := range p.marked {
		if !names[n] {
			delete(p.marked, n)
		}
	}
	p.clamp()
}

func (p *pane) refilter() {
	vfs.Sort(p.all, p.sortBy)
	p.visible = p.visible[:0]
	if p.fs != nil && p.fs.Dir(p.cwd) != p.cwd {
		p.visible = append(p.visible, vfs.Entry{Name: parentName, IsDir: true})
	}
	f := strings.ToLower(p.filter)
	for _, e := range p.all {
		if !p.showHidden && e.Hidden() {
			continue
		}
		if f != "" && !strings.Contains(strings.ToLower(e.Name), f) {
			continue
		}
		p.visible = append(p.visible, e)
	}
	p.clamp()
}

func (p *pane) selectName(name string) {
	for i, e := range p.visible {
		if e.Name == name {
			p.cursor = i
			p.clamp()
			return
		}
	}
}

func (p *pane) selected() (vfs.Entry, bool) {
	if p.cursor < 0 || p.cursor >= len(p.visible) {
		return vfs.Entry{}, false
	}
	return p.visible[p.cursor], true
}

// targets returns the marked entries, or the selected one. ".." is never
// a target.
func (p *pane) targets() []vfs.Entry {
	var out []vfs.Entry
	if len(p.marked) > 0 {
		for _, e := range p.all {
			if p.marked[e.Name] {
				out = append(out, e)
			}
		}
		return out
	}
	if e, ok := p.selected(); ok && e.Name != parentName {
		out = append(out, e)
	}
	return out
}

func (p *pane) paths(entries []vfs.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = p.fs.Join(p.cwd, e.Name)
	}
	return out
}

func (p *pane) toggleMark() {
	e, ok := p.selected()
	if !ok || e.Name == parentName {
		return
	}
	if p.marked[e.Name] {
		delete(p.marked, e.Name)
	} else {
		p.marked[e.Name] = true
	}
}

func (p *pane) markAll() {
	all := true
	for _, e := range p.visible {
		if e.Name != parentName && !p.marked[e.Name] {
			all = false
		}
	}
	p.marked = map[string]bool{}
	if all {
		return // toggle off
	}
	for _, e := range p.visible {
		if e.Name != parentName {
			p.marked[e.Name] = true
		}
	}
}

func (p *pane) move(d int) {
	p.cursor += d
	p.clamp()
}

func (p *pane) listHeight() int { return max(1, p.height-3) } // border + title

func (p *pane) clamp() {
	p.cursor = max(0, min(p.cursor, len(p.visible)-1))
	h := p.listHeight()
	if p.cursor < p.offset {
		p.offset = p.cursor
	}
	if p.cursor >= p.offset+h {
		p.offset = p.cursor - h + 1
	}
	p.offset = max(0, min(p.offset, len(p.visible)-h))
}

// displayPath abbreviates the home directory for local panes.
func (p *pane) displayPath() string {
	if p.fs != nil && !p.fs.Remote() {
		if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p.cwd, home) {
			return "~" + strings.TrimPrefix(p.cwd, home)
		}
	}
	return p.cwd
}

func (p *pane) view(t theme.Theme, focused bool, now time.Time) string {
	border := t.Border
	if focused {
		border = border.BorderForeground(t.Accent)
	}
	inner := max(10, p.width-border.GetHorizontalFrameSize())

	titleStyle := lipgloss.NewStyle().Bold(true)
	if focused {
		titleStyle = titleStyle.Foreground(t.Accent)
	}
	title := p.label
	if p.cwd != "" {
		title += ": " + p.displayPath()
	}
	info := ""
	if p.filter != "" {
		info += " /" + p.filter
	}
	if n := len(p.marked); n > 0 {
		info += fmt.Sprintf(" · %d marked", n)
	}
	head := titleStyle.Render(ansi.TruncateLeft(title, max(0, lipgloss.Width(title)-(inner-lipgloss.Width(info))), "…")) +
		t.Faint.Render(info)

	var lines []string
	switch {
	case p.err != "":
		lines = append(lines, t.Error.Render(ansi.Wrap(p.err, inner, "")))
	case p.loading && len(p.visible) == 0:
		lines = append(lines, t.Faint.Render("Loading…"))
	case len(p.visible) == 0:
		lines = append(lines, t.Faint.Render("(empty)"))
	}
	h := p.listHeight()
	if p.err == "" {
		end := min(len(p.visible), p.offset+h)
		for i := p.offset; i < end; i++ {
			lines = append(lines, p.row(t, p.visible[i], i == p.cursor && focused, i == p.cursor, inner, now))
		}
	}
	body := lipgloss.NewStyle().Width(inner).Height(h).MaxHeight(h).Render(strings.Join(lines, "\n"))
	return border.Width(p.width).Render(head + "\n" + body)
}

func (p *pane) row(t theme.Theme, e vfs.Entry, active, cursor bool, w int, now time.Time) string {
	mark := " "
	if p.marked[e.Name] {
		mark = lipgloss.NewStyle().Foreground(t.Warning).Bold(true).Render("*")
	}
	icon := "  "
	name := e.Name
	switch {
	case e.Name == parentName:
		icon = "  "
	case e.Link != "" && e.Broken:
		icon = "! "
	case e.Link != "" && e.IsDir:
		icon = "↪ "
		name += "/"
	case e.Link != "":
		icon = "↪ "
	case e.IsDir:
		icon = "▸ "
		name += "/"
	}

	size, date := "", ""
	if e.Name != parentName {
		if e.IsDir {
			size = "—"
		} else {
			size = transfer.FormatBytes(e.Size)
		}
		date = formatDate(e.ModTime, now)
	}
	right := fmt.Sprintf("%7s  %-9s", size, date)
	nameW := max(4, w-2-lipgloss.Width(icon)-lipgloss.Width(right)-1)
	name = ansi.Truncate(name, nameW, "…")
	pad := strings.Repeat(" ", max(0, nameW-lipgloss.Width(name)))

	style := lipgloss.NewStyle()
	switch {
	case e.IsDir:
		style = style.Foreground(t.Accent)
	case e.Broken:
		style = style.Foreground(t.Danger)
	case e.Hidden():
		style = style.Foreground(t.Muted)
	}
	if active {
		return lipgloss.NewStyle().Background(t.Subtle).Bold(true).Width(w).
			Render(mark + " " + icon + style.Render(name) + pad + " " + right)
	}
	if cursor { // cursor of the unfocused pane
		return mark + "›" + icon + style.Render(name) + pad + " " + t.Faint.Render(right)
	}
	return mark + " " + icon + style.Render(name) + pad + " " + t.Faint.Render(right)
}

func formatDate(t, now time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	if t.Year() == now.Year() {
		return t.Format("02 Jan")
	}
	return t.Format("Jan 2006")
}
