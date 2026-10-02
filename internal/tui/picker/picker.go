// Package picker is the minimal host chooser used when a query is ambiguous.
package picker

import (
	"errors"
	"os"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/ProductionPanic/rootnet-cli/internal/store"
)

// ErrCancelled is returned when the user quits without choosing a host.
var ErrCancelled = errors.New("cancelled")

var docStyle = lipgloss.NewStyle().Margin(1, 2)

type item struct{ host store.Host }

func (i item) Title() string { return i.host.Name }
func (i item) Description() string {
	d := i.host.Target()
	if i.host.Environment != "" {
		d += "  [" + i.host.Environment + "]"
	}
	if len(i.host.Tags) > 0 {
		d += "  #" + strings.Join(i.host.Tags, " #")
	}
	return d
}
func (i item) FilterValue() string {
	return i.host.Name + " " + i.host.Target() + " " + strings.Join(i.host.Tags, " ")
}

type model struct {
	list   list.Model
	choice *store.Host
}

func (m model) Init() tea.Cmd { return tea.RequestBackgroundColor }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		d := list.NewDefaultDelegate()
		d.Styles = list.NewDefaultItemStyles(msg.IsDark())
		m.list.SetDelegate(d)
		m.list.Styles = list.DefaultStyles(msg.IsDark())
	case tea.KeyPressMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.list.FilterState() == list.Filtering {
				break
			}
			if i, ok := m.list.SelectedItem().(item); ok {
				h := i.host
				m.choice = &h
			}
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) View() tea.View {
	v := tea.NewView(docStyle.Render(m.list.View()))
	v.AltScreen = true
	return v
}

// Pick shows hosts in a filterable list and returns the chosen one. The UI
// is rendered on stderr so stdout stays clean for scripting.
func Pick(title string, hosts []store.Host) (store.Host, error) {
	items := make([]list.Item, len(hosts))
	for i, h := range hosts {
		items[i] = item{host: h}
	}
	l := list.New(items, list.NewDefaultDelegate(), 0, 0)
	l.Title = title
	l.SetStatusBarItemName("host", "hosts")

	final, err := tea.NewProgram(model{list: l}, tea.WithOutput(os.Stderr)).Run()
	if err != nil {
		return store.Host{}, err
	}
	if c := final.(model).choice; c != nil {
		return *c, nil
	}
	return store.Host{}, ErrCancelled
}
