package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const hostsPath = "/rootnet_hosts.txt"

// -- UI Model Logic --

var docStyle = lipgloss.NewStyle().Margin(1, 2)

type item struct {
	title, desc, host string
}

func (i item) Title() string       { return i.title }
func (i item) Description() string { return i.desc }
func (i item) FilterValue() string { return i.title + " " + i.desc }

type model struct {
	list     list.Model
	choice   string
	quitting bool
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
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
		if msg.String() == "enter" && m.list.FilterState() != list.Filtering {
			i, ok := m.list.SelectedItem().(item)
			if ok {
				m.choice = i.host
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
// -- Application Logic --

func getHostsFile() string {
	home, _ := os.UserHomeDir()
	return home + hostsPath
}

func loadItems() []list.Item {
	var items []list.Item
	file, err := os.Open(getHostsFile())
	if err != nil {
		return items
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if parts := strings.Split(line, "|"); len(parts) >= 2 {
			name := strings.TrimSpace(parts[0])
			host := strings.TrimSpace(parts[1])
			items = append(items, item{title: name, desc: host, host: host})
		}
	}
	return items
}

func main() {
	arg := ""
	if len(os.Args) > 1 {
		arg = os.Args[1]
	}

	switch arg {
	case "get":
		search := ""
		if len(os.Args) > 2 {
			search = os.Args[2]
		}
		fmt.Print(runFilter(search, true))
	default:
		// SSH mode
		host := runFilter(arg, false)
		if host != "" {
			runSSH(host)
		}
	}
}

func runFilter(search string, outputOnly bool) string {
	items := loadItems()
	var matches []item

	// 1. Check for partial matches
	if search != "" {
		for _, i := range items {
			itm := i.(item)
			// Check if search term exists in Title or Description (case-insensitive)
			if strings.Contains(strings.ToLower(itm.title), strings.ToLower(search)) ||
				strings.Contains(strings.ToLower(itm.desc), strings.ToLower(search)) {
				matches = append(matches, itm)
			}
		}

		// 2. If exactly one partial match is found, return it immediately
		if len(matches) == 1 {
			return matches[0].host
		}
	}

	// 3. If multiple matches found, narrow the list for the UI
	// If no matches found, show the full list
	uiItems := items
	if len(matches) > 1 {
		uiItems = make([]list.Item, len(matches))
		for i, m := range matches {
			uiItems[i] = m
		}
	}

	// 4. Launch the Bubble Tea TUI
	m := model{list: list.New(uiItems, list.NewDefaultDelegate(), 0, 0)}
	m.list.Title = "Rootnet Projects"

	// Render the TUI to stderr so that stdout only ever carries the result.
	// This keeps `ssh $(rootnet get foo)` working when the picker opens.
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	finalModel, err := p.Run()
	if err != nil {
		return ""
	}

	return finalModel.(model).choice
}

func runSSH(host string) {
	cmd := exec.Command("ssh", host)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}
