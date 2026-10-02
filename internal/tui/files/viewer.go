package files

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/ProductionPanic/rootnet-cli/v2/internal/transfer"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/tui/theme"
	"github.com/ProductionPanic/rootnet-cli/v2/internal/vfs"
)

// viewLimit is how much of a file the viewer reads.
const viewLimit = 1 << 20

type viewerLoadedMsg struct {
	path      string
	raw       []string
	styled    []string
	truncated bool
	binary    bool
	err       error
}

// loadForView reads up to viewLimit bytes and prepares the lines off the
// UI goroutine.
func loadForView(fsys vfs.FS, path string, dark bool) tea.Cmd {
	return func() tea.Msg {
		r, err := fsys.Open(path)
		if err != nil {
			return viewerLoadedMsg{path: path, err: err}
		}
		defer r.Close()
		data, err := io.ReadAll(io.LimitReader(r, viewLimit+1))
		if err != nil {
			return viewerLoadedMsg{path: path, err: err}
		}
		msg := viewerLoadedMsg{path: path}
		if len(data) > viewLimit {
			data, msg.truncated = data[:viewLimit], true
		}
		if isBinary(data) {
			msg.binary = true
			return msg
		}
		text := strings.ReplaceAll(strings.ReplaceAll(string(data), "\r\n", "\n"), "\t", "    ")
		if msg.truncated {
			// Don't show a half line at the cut.
			if i := strings.LastIndexByte(text, '\n'); i > 0 {
				text = text[:i]
			}
		}
		text = strings.TrimSuffix(text, "\n")
		msg.raw = strings.Split(text, "\n")
		if styled := highlightLines(fsys.Base(path), text, dark); len(styled) == len(msg.raw) {
			msg.styled = styled
		}
		return msg
	}
}

// isBinary guesses whether data is binary: a NUL byte in the first 8 KiB,
// or invalid UTF-8 that isn't just a cut-off multi-byte character.
func isBinary(data []byte) bool {
	head := data[:min(len(data), 8000)]
	if bytes.IndexByte(head, 0) >= 0 {
		return true
	}
	for len(head) > 0 {
		r, size := utf8.DecodeRune(head)
		if r == utf8.RuneError && size == 1 {
			return len(head) > 3 // allow a truncated rune at the very end
		}
		head = head[size:]
	}
	return false
}

type viewerKeys struct {
	Up, Down, HalfDown, HalfUp, Top, Bottom key.Binding
	Search, Next, Prev, Wrap, Edit, Close   key.Binding
}

func newViewerKeys() viewerKeys {
	return viewerKeys{
		Up:       key.NewBinding(key.WithKeys("k", "up"), key.WithHelp("j/k", "scroll")),
		Down:     key.NewBinding(key.WithKeys("j", "down", "enter")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d", "pgdown", "space", "f"), key.WithHelp("ctrl+d/u", "page")),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u", "pgup", "b")),
		Top:      key.NewBinding(key.WithKeys("g", "home"), key.WithHelp("g/G", "top/bottom")),
		Bottom:   key.NewBinding(key.WithKeys("G", "end")),
		Search:   key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Next:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n/N", "next/prev")),
		Prev:     key.NewBinding(key.WithKeys("N")),
		Wrap:     key.NewBinding(key.WithKeys("w"), key.WithHelp("w", "wrap")),
		Edit:     key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "edit")),
		Close:    key.NewBinding(key.WithKeys("q", "esc"), key.WithHelp("q", "close")),
	}
}

func (k viewerKeys) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.HalfDown, k.Top, k.Search, k.Next, k.Wrap, k.Edit, k.Close}
}
func (k viewerKeys) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }

// viewer is a read-only, full-screen file view.
type viewer struct {
	keys  viewerKeys
	side  int
	entry vfs.Entry
	path  string
	label string // host or "Local"

	loading   bool
	err       string
	binary    bool
	truncated bool
	raw       []string
	styled    []string

	wrap    bool
	display []dline
	top     int

	search    textinput.Model
	searching bool
	query     string
	matches   []int // line indexes
	match     int

	width, height int
}

// dline is one screen row: a (part of a) source line.
type dline struct {
	n     int // source line index
	first bool
	text  string
}

func newViewer(side int, label string, e vfs.Entry, path string) *viewer {
	ti := textinput.New()
	ti.Prompt = "/"
	return &viewer{keys: newViewerKeys(), side: side, entry: e, path: path, label: label, loading: true, search: ti}
}

func (v *viewer) loaded(msg viewerLoadedMsg) {
	v.loading = false
	if msg.err != nil {
		v.err = msg.err.Error()
		return
	}
	v.binary, v.truncated, v.raw, v.styled = msg.binary, msg.truncated, msg.raw, msg.styled
	v.layout()
}

func (v *viewer) setSize(w, h int) {
	v.width, v.height = w, h
	v.layout()
}

func (v *viewer) bodyHeight() int { return max(1, v.height-2) } // header + help

func (v *viewer) gutterWidth() int { return len(fmt.Sprint(len(v.raw))) + 1 }

func (v *viewer) layout() {
	v.display = v.display[:0]
	if len(v.raw) == 0 {
		return
	}
	textW := max(10, v.width-v.gutterWidth()-1)
	q := strings.ToLower(v.query)
	for i, raw := range v.raw {
		text := raw
		if q != "" && strings.Contains(strings.ToLower(raw), q) {
			text = markMatches(raw, q)
		} else if v.styled != nil {
			text = v.styled[i]
		}
		if !v.wrap {
			v.display = append(v.display, dline{n: i, first: true, text: ansi.Truncate(text, textW, "…")})
			continue
		}
		for j, part := range strings.Split(ansi.Hardwrap(text, textW, true), "\n") {
			v.display = append(v.display, dline{n: i, first: j == 0, text: part})
		}
	}
	v.clamp()
}

// markMatches renders raw with every case-insensitive occurrence of q in
// reverse video.
func markMatches(raw, q string) string {
	lower := strings.ToLower(raw)
	if len(lower) != len(raw) { // case mapping changed byte lengths; keep it simple
		return "\x1b[7m" + raw + "\x1b[0m"
	}
	var b strings.Builder
	for i := 0; ; {
		j := strings.Index(lower[i:], q)
		if j < 0 {
			b.WriteString(raw[i:])
			return b.String()
		}
		b.WriteString(raw[i : i+j])
		b.WriteString("\x1b[7m" + raw[i+j:i+j+len(q)] + "\x1b[0m")
		i += j + len(q)
	}
}

func (v *viewer) clamp() {
	v.top = max(0, min(v.top, len(v.display)-v.bodyHeight()))
}

func (v *viewer) scrollToLine(n int) {
	for i, d := range v.display {
		if d.n == n && d.first {
			v.top = i - v.bodyHeight()/3
			v.clamp()
			return
		}
	}
}

func (v *viewer) runSearch(q string) {
	v.query = q
	v.matches = v.matches[:0]
	v.match = 0
	if q != "" {
		lq := strings.ToLower(q)
		for i, raw := range v.raw {
			if strings.Contains(strings.ToLower(raw), lq) {
				v.matches = append(v.matches, i)
			}
		}
	}
	v.layout()
	// Jump to the first match below the current view.
	cur := 0
	if v.top < len(v.display) {
		cur = v.display[v.top].n
	}
	for i, n := range v.matches {
		if n >= cur {
			v.match = i
			break
		}
	}
	if len(v.matches) > 0 {
		v.scrollToLine(v.matches[v.match])
	}
}

// update handles keys; it reports close/edit requests.
func (v *viewer) update(k tea.KeyPressMsg) (closeIt, edit bool, cmd tea.Cmd) {
	if v.searching {
		switch k.String() {
		case "enter":
			v.searching = false
			v.search.Blur()
			v.runSearch(v.search.Value())
		case "esc":
			v.searching = false
			v.search.Blur()
		default:
			v.search, cmd = v.search.Update(k)
		}
		return false, false, cmd
	}
	page := v.bodyHeight()
	switch {
	case key.Matches(k, v.keys.Close):
		if k.String() == "esc" && v.query != "" {
			v.runSearch("")
			return
		}
		return true, false, nil
	case key.Matches(k, v.keys.Edit):
		return false, true, nil
	case key.Matches(k, v.keys.Up):
		v.top--
	case key.Matches(k, v.keys.Down):
		v.top++
	case key.Matches(k, v.keys.HalfDown):
		v.top += page / 2
	case key.Matches(k, v.keys.HalfUp):
		v.top -= page / 2
	case key.Matches(k, v.keys.Top):
		v.top = 0
	case key.Matches(k, v.keys.Bottom):
		v.top = len(v.display)
	case key.Matches(k, v.keys.Wrap):
		v.wrap = !v.wrap
		v.layout()
	case key.Matches(k, v.keys.Search):
		v.searching = true
		v.search.SetValue("")
		return false, false, v.search.Focus()
	case key.Matches(k, v.keys.Next), key.Matches(k, v.keys.Prev):
		if len(v.matches) > 0 {
			d := 1
			if key.Matches(k, v.keys.Prev) {
				d = -1
			}
			v.match = (v.match + d + len(v.matches)) % len(v.matches)
			v.scrollToLine(v.matches[v.match])
		}
	}
	v.clamp()
	return false, false, nil
}

func (v *viewer) view(t theme.Theme, helpView func(viewerKeys) string) string {
	title := lipgloss.NewStyle().Bold(true).Foreground(t.Accent).Render(v.label + ": " + v.path)
	var info []string
	info = append(info, transfer.FormatBytes(v.entry.Size))
	if len(v.raw) > 0 {
		if len(v.raw) == 1 {
			info = append(info, "1 line")
		} else {
			info = append(info, fmt.Sprintf("%d lines", len(v.raw)))
		}
	}
	if v.truncated {
		info = append(info, "showing first 1 MB")
	}
	if v.query != "" {
		if len(v.matches) == 0 {
			info = append(info, fmt.Sprintf("no match for %q", v.query))
		} else {
			info = append(info, fmt.Sprintf("match %d/%d", v.match+1, len(v.matches)))
		}
	}
	header := ansi.Truncate(title+"  "+t.Faint.Render(strings.Join(info, " · ")), v.width, "…")

	h := v.bodyHeight()
	var body []string
	switch {
	case v.loading:
		body = append(body, t.Faint.Render("Loading…"))
	case v.err != "":
		body = append(body, t.Error.Render(v.err))
	case v.binary:
		body = append(body, t.Faint.Render("Binary file — not shown. Press e to open it in your editor anyway."))
	case len(v.raw) == 1 && v.raw[0] == "":
		body = append(body, t.Faint.Render("(empty file)"))
	default:
		gw := v.gutterWidth()
		gutter := lipgloss.NewStyle().Foreground(t.Muted)
		matchGutter := lipgloss.NewStyle().Foreground(t.Warning).Bold(true)
		cur := -1
		if len(v.matches) > 0 {
			cur = v.matches[v.match]
		}
		end := min(len(v.display), v.top+h)
		for i := v.top; i < end; i++ {
			d := v.display[i]
			num := strings.Repeat(" ", gw)
			if d.first {
				num = fmt.Sprintf("%*d ", gw-1, d.n+1)
			}
			g := gutter
			if d.n == cur {
				g = matchGutter
			}
			body = append(body, g.Render(num)+" "+d.text)
		}
	}
	for len(body) < h {
		body = append(body, "")
	}

	footer := helpView(v.keys)
	if v.searching {
		footer = v.search.View()
	}
	return header + "\n" + strings.Join(body, "\n") + "\n" + footer
}
